import * as fs from 'fs';
import * as path from 'path';
import * as cdk from '@aws-cdk/core';
import * as iot from '@aws-cdk/aws-iot'
import * as lambda from '@aws-cdk/aws-lambda'
import * as iam from '@aws-cdk/aws-iam'
import * as logs from '@aws-cdk/aws-logs';
import * as cr from '@aws-cdk/custom-resources';

export class InfraStack extends cdk.Stack {
  constructor(scope: cdk.Construct, id: string, props?: cdk.StackProps) {
    super(scope, id, props);

    const certificateArn = new cdk.CfnParameter(this, "certificateArn", {
      type: "String",
      description: "IoT Certificate ARN taken from the create-keys-and-certs.sh script"
    });

    const fn = new lambda.Function(this, 'PreProvisionHookFunction', {
      functionName: `iot-pre-provision-hook`,
      code: lambda.Code.fromAsset(path.resolve(__dirname, 'lambda', 'pre-provision-hook')),
      runtime: lambda.Runtime.PYTHON_3_8,
      handler: 'lambda.lambda_handler',
      memorySize: 256,
      description: 'Pre Provision Hook for IoT Thing Provisioning',
      timeout: cdk.Duration.seconds(30)
    });

    fn.addToRolePolicy(new iam.PolicyStatement({
      actions: [
        'logs:CreateLogGroup',
        'logs:CreateLogStream',
        'logs:PutLogEvents'
      ],
      effect: iam.Effect.ALLOW,
      resources: ['arn:aws:logs:*:*:*'],
    }));
    fn.grantInvoke(new iam.ServicePrincipal('iot.amazonaws.com'));

    const iotProvisioningHookRole = new iam.Role(this, 'iotProvisioningHookRole', {
      assumedBy: new iam.ServicePrincipal('iot.amazonaws.com'),
      managedPolicies: [
        iam.ManagedPolicy.fromAwsManagedPolicyName('service-role/AWSIoTThingsRegistration'),
        iam.ManagedPolicy.fromAwsManagedPolicyName('service-role/AWSIoTLogging'),
        iam.ManagedPolicy.fromAwsManagedPolicyName('AmazonS3ReadOnlyAccess'),
        iam.ManagedPolicy.fromAwsManagedPolicyName('service-role/AWSIoTRuleActions'),
      ],
      inlinePolicies: {
        IoTAddOnPolicy: new iam.PolicyDocument({
          statements: [
            new iam.PolicyStatement({
              effect: iam.Effect.ALLOW,
              actions: [
                "iotsitewise:BatchPutAssetPropertyValue",
                "iotanalytics:BatchPutMessage",
                "iotevents:BatchPutMessage"
              ],
              resources: [
                "*"
              ]
            })
          ]
        })
      }
    });

    const templateBody = fs.readFileSync(path.join(__dirname, "templates", "fleet_template.json"), 'utf8');
    const templateName = 'GoFleetProvisioningTemplate'

    new iot.CfnProvisioningTemplate(this, 'FleetProvisioningTemplate', {
      provisioningRoleArn: iotProvisioningHookRole.roleArn,
      templateBody: templateBody,
      description: "Fleet Provisioning Template for Go IoT Devices",
      enabled: true,
      preProvisioningHook: {
        payloadVersion: "2020-04-01",
        targetArn: fn.functionArn
      },
      templateName: templateName
    });

    const iotFleetPolicy = new iot.CfnPolicy(this, 'FleetProvisioningPolicy', {
      policyDocument: {
        Version: '2012-10-17',
        Statement: [
          {
            "Effect": "Allow",
            "Action": ["iot:Connect"],
            "Resource": "*"
          },
          {
              "Effect": "Allow",
              "Action": ["iot:Publish","iot:Receive"],
              "Resource": [
                  `arn:aws:iot:${cdk.Aws.REGION}:${cdk.Aws.ACCOUNT_ID}:topic/$aws/certificates/create/*`,
                  `arn:aws:iot:${cdk.Aws.REGION}:${cdk.Aws.ACCOUNT_ID}:topic/$aws/provisioning-templates/${templateName}/provision/*`
              ]
          },
          {
              "Effect": "Allow",
              "Action": "iot:Subscribe",
              "Resource": [
                  `arn:aws:iot:${cdk.Aws.REGION}:${cdk.Aws.ACCOUNT_ID}:topicfilter/$aws/certificates/create/*`,
                  `arn:aws:iot:${cdk.Aws.REGION}:${cdk.Aws.ACCOUNT_ID}:topicfilter/$aws/provisioning-templates/${templateName}/provision/*`
              ]
          }
        ],
      },
      policyName: 'GoIoTFleetProvisioningPolicy'
    })

    const iotPolicyPrincipalAttachment = new iot.CfnPolicyPrincipalAttachment(this, 'IotPolicyPrincipalAttachment', {
        policyName: iotFleetPolicy.policyName!,
        principal: certificateArn.valueAsString,
      }
    )
    iotPolicyPrincipalAttachment.addDependsOn(iotFleetPolicy)

    const logsPolicy = new iam.PolicyStatement({
      resources: [`arn:${cdk.Aws.PARTITION}:logs:${cdk.Aws.REGION}:${cdk.Aws.ACCOUNT_ID}:log-group:/aws/lambda/*`],
      actions: ['logs:CreateLogGroup', 'logs:CreateLogStream', 'logs:PutLogEvents']
    });

    const iotPolicy = new iam.PolicyStatement({
      resources: ['*'],
      actions: ['iot:CreateThingGroup', 'iot:DeleteThingGroup']
    });

    const inlinePolicies = {
        CloudWatchLogsPolicy: new iam.PolicyDocument({
          statements: [logsPolicy]
        }),
        IotThingPolicy: new iam.PolicyDocument({
          statements: [iotPolicy]
        })
    };

    const customResourceRole = new iam.Role(this, 'Role', {
        assumedBy: new iam.ServicePrincipal('lambda.amazonaws.com'),
        inlinePolicies
    });

    const onEvent = new lambda.Function(this, 'ThingGroupCreationHandler', {
      runtime: lambda.Runtime.PYTHON_3_8,
      code: lambda.Code.fromAsset(path.join(__dirname, 'lambda/iot-cr-handler')),
      handler: 'index.on_event',
      role: customResourceRole,
    });

    const iotProvider = new cr.Provider(this, 'CRProvider', {
      onEventHandler: onEvent,
      logRetention: logs.RetentionDays.ONE_DAY,
    });

    const outputs = new cdk.CustomResource(this, 'StackOutputs', {
      serviceToken: iotProvider.serviceToken,
      properties: {
        stackName: cdk.Stack.name,
        regionName: cdk.Aws.REGION,
        thingGroupName: 'fleet-provisioning-group'
      },
    });
  }
}
