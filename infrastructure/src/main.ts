import * as fs from 'fs';
import path from 'path';
import { App, Aws, CfnParameter, CustomResource, Duration, Stack, StackProps } from 'aws-cdk-lib';
import { Effect, ManagedPolicy, PolicyDocument, PolicyStatement, Role, ServicePrincipal } from 'aws-cdk-lib/aws-iam';
import { CfnPolicy, CfnPolicyPrincipalAttachment, CfnProvisioningTemplate } from 'aws-cdk-lib/aws-iot';
import { Code, Function, Runtime } from 'aws-cdk-lib/aws-lambda';
import { RetentionDays } from 'aws-cdk-lib/aws-logs';
import { Provider } from 'aws-cdk-lib/custom-resources';
import { Construct } from 'constructs';

export class InfraStack extends Stack {
  constructor(scope: Construct, id: string, props?: StackProps) {
    super(scope, id, props);

    const certificateArn = new CfnParameter(this, 'certificateArn', {
      type: 'String',
      description: 'IoT Certificate ARN taken from the create-keys-and-certs.sh script',
    });

    const fn = new Function(this, 'PreProvisionHookFunction', {
      functionName: 'iot-pre-provision-hook',
      code: Code.fromAsset(path.resolve(__dirname, '../lambda', 'pre-provision-hook')),
      runtime: Runtime.PYTHON_3_8,
      handler: 'lambda.lambda_handler',
      memorySize: 256,
      description: 'Pre Provision Hook for IoT Thing Provisioning',
      timeout: Duration.seconds(30),
    });

    fn.addToRolePolicy(new PolicyStatement({
      actions: [
        'logs:CreateLogGroup',
        'logs:CreateLogStream',
        'logs:PutLogEvents',
      ],
      effect: Effect.ALLOW,
      resources: ['arn:aws:logs:*:*:*'],
    }));
    fn.grantInvoke(new ServicePrincipal('iot.amazonaws.com'));

    const iotProvisioningHookRole = new Role(this, 'iotProvisioningHookRole', {
      assumedBy: new ServicePrincipal('iot.amazonaws.com'),
      managedPolicies: [
        ManagedPolicy.fromAwsManagedPolicyName('service-role/AWSIoTThingsRegistration'),
        ManagedPolicy.fromAwsManagedPolicyName('service-role/AWSIoTLogging'),
        ManagedPolicy.fromAwsManagedPolicyName('AmazonS3ReadOnlyAccess'),
        ManagedPolicy.fromAwsManagedPolicyName('service-role/AWSIoTRuleActions'),
      ],
      inlinePolicies: {
        IoTAddOnPolicy: new PolicyDocument({
          statements: [
            new PolicyStatement({
              effect: Effect.ALLOW,
              actions: [
                'iotsitewise:BatchPutAssetPropertyValue',
                'iotanalytics:BatchPutMessage',
                'iotevents:BatchPutMessage',
              ],
              resources: [
                '*',
              ],
            }),
          ],
        }),
      },
    });

    const templateBody = fs.readFileSync(path.join(__dirname, '../templates', 'fleet_template.json'), 'utf8');
    const templateName = 'GoFleetProvisioningTemplate';

    new CfnProvisioningTemplate(this, 'FleetProvisioningTemplate', {
      provisioningRoleArn: iotProvisioningHookRole.roleArn,
      templateBody: templateBody,
      description: 'Fleet Provisioning Template for Go IoT Devices',
      enabled: true,
      preProvisioningHook: {
        payloadVersion: '2020-04-01',
        targetArn: fn.functionArn,
      },
      templateName: templateName,
    });

    const iotFleetPolicy = new CfnPolicy(this, 'FleetProvisioningPolicy', {
      policyDocument: {
        Version: '2012-10-17',
        Statement: [
          {
            Effect: 'Allow',
            Action: ['iot:Connect'],
            Resource: '*',
          },
          {
            Effect: 'Allow',
            Action: ['iot:Publish', 'iot:Receive'],
            Resource: [
              `arn:aws:iot:${Aws.REGION}:${Aws.ACCOUNT_ID}:topic/$aws/certificates/create/*`,
              `arn:aws:iot:${Aws.REGION}:${Aws.ACCOUNT_ID}:topic/$aws/provisioning-templates/${templateName}/provision/*`,
            ],
          },
          {
            Effect: 'Allow',
            Action: 'iot:Subscribe',
            Resource: [
              `arn:aws:iot:${Aws.REGION}:${Aws.ACCOUNT_ID}:topicfilter/$aws/certificates/create/*`,
              `arn:aws:iot:${Aws.REGION}:${Aws.ACCOUNT_ID}:topicfilter/$aws/provisioning-templates/${templateName}/provision/*`,
            ],
          },
        ],
      },
      policyName: 'GoIoTFleetProvisioningPolicy',
    });

    const iotPolicyPrincipalAttachment = new CfnPolicyPrincipalAttachment(this, 'IotPolicyPrincipalAttachment', {
      policyName: iotFleetPolicy.policyName!,
      principal: certificateArn.valueAsString,
    },
    );
    iotPolicyPrincipalAttachment.addDependsOn(iotFleetPolicy);

    const logsPolicy = new PolicyStatement({
      resources: [`arn:${Aws.PARTITION}:logs:${Aws.REGION}:${Aws.ACCOUNT_ID}:log-group:/aws/lambda/*`],
      actions: ['logs:CreateLogGroup', 'logs:CreateLogStream', 'logs:PutLogEvents'],
    });

    const iotPolicy = new PolicyStatement({
      resources: ['*'],
      actions: ['iot:CreateThingGroup', 'iot:DeleteThingGroup'],
    });

    const inlinePolicies = {
      CloudWatchLogsPolicy: new PolicyDocument({
        statements: [logsPolicy],
      }),
      IotThingPolicy: new PolicyDocument({
        statements: [iotPolicy],
      }),
    };

    const customResourceRole = new Role(this, 'Role', {
      assumedBy: new ServicePrincipal('lambda.amazonaws.com'),
      inlinePolicies,
    });

    const onEvent = new Function(this, 'ThingGroupCreationHandler', {
      runtime: Runtime.PYTHON_3_8,
      code: Code.fromAsset(path.join(__dirname, '../lambda/iot-cr-handler')),
      handler: 'index.on_event',
      role: customResourceRole,
    });

    const iotProvider = new Provider(this, 'CRProvider', {
      onEventHandler: onEvent,
      logRetention: RetentionDays.ONE_DAY,
    });

    new CustomResource(this, 'StackOutputs', {
      serviceToken: iotProvider.serviceToken,
      properties: {
        stackName: Stack.name,
        regionName: Aws.REGION,
        thingGroupName: 'fleet-provisioning-group',
      },
    });
  }
}

// for development, use account/region from cdk cli
const devEnv = {
  account: process.env.CDK_DEFAULT_ACCOUNT,
  region: process.env.CDK_DEFAULT_REGION,
};

const app = new App();

new InfraStack(app, 'infrastructure-dev', { env: devEnv });
// new MyStack(app, 'infrastructure-prod', { env: prodEnv });

app.synth();