import * as fs from 'fs';
import * as path from 'path';
import * as cdk from '@aws-cdk/core';
import * as iot from '@aws-cdk/aws-iot'
import * as lambda from '@aws-cdk/aws-lambda'
import * as iam from '@aws-cdk/aws-iam'

export class InfraStack extends cdk.Stack {
  constructor(scope: cdk.Construct, id: string, props?: cdk.StackProps) {
    super(scope, id, props);

    const certificateArn = new cdk.CfnParameter(this, "certificateArn", {
      type: "String",
      description: "IoT Certificate ARN taken from the create-keys-and-certs.sh script"
    });

    const ns = scope.node.tryGetContext('ns') || '';

    const fn = new lambda.Function(this, 'PreProvisionHookFunction', {
      functionName: `${ns}-pre-provision-hook`,
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
                  "arn:aws:iot:us-east-1:649037252677:topic/$aws/certificates/create/*",
                  `arn:aws:iot:us-east-1:649037252677:topic/$aws/provisioning-templates/${templateName}/provision/*`
              ]
          },
          {
              "Effect": "Allow",
              "Action": "iot:Subscribe",
              "Resource": [
                  "arn:aws:iot:us-east-1:649037252677:topicfilter/$aws/certificates/create/*",
                  `arn:aws:iot:us-east-1:649037252677:topicfilter/$aws/provisioning-templates/${templateName}/provision/*`
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
  }
}
