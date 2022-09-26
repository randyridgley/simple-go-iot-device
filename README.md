# Simple AWS IoT Device

Simple example to bootstrap Golang device with the AWS IoT Core service.  

## Getting Started

To get started we will need to setup the infrastucture required to provision and bootstrap the IoT thing.
The first thing to do is create the latest root-ca bundle if you don't already have it available. A convienence script is loacted in the scripts directory `create-root-ca-bundle.sh`. When you execute the script it will create a `root.ca.bundle.pem` file in the `certs` directory. 

To run the script in the `scripts` directory ensure it has execute permissions and you should see something like the output below:

``` bash
    > chmod +x create-root-ca-bundle.sh
    > ./create-root-ca-bundle.sh

    ...
    Length: 1758 (1.7K) [application/octet-stream]
    Saving to: ‘STDOUT’

    - 100%[======================================================================>] 1.72K --.-KB/s in 0s      

    2021-03-04 09:03:51 (15.7 MB/s) - written to stdout [1758/1758]

    Stored CA certificates in ../certs/root.ca.bundle-test.pem
```

The next step will be to create the bootstrap keys and certificate that will be used to register the iot device with the IoT core services.
In order to do so go to the `scripts` directory and find the file `create-keys-and-cert.sh`. 

To run the script in the `scripts` directory ensure it has execute permissions and you should see something like the output below:

``` bash
    > chmod +x create-keys-and-certs.sh
    > ./create-keys-and-certs.sh

    >Creating the keys and certificate
     arn:aws:iot:REGION:ACCOUNT_ID:cert/XXXXXXXXXXX
```

Save the `certificate_arn` that was returned from the script for later use when setting up the infrastructure required with CDK. This also created the files below in the `certs` directory. For more information on what this script created you can find it in the [CLI Reference](https://docs.aws.amazon.com/cli/latest/reference/iot/create-keys-and-certificate.html).

* fleet-provisioning.certificate.pem
* fleet-provisioning.private.key
* fleet-provisioning.public.key

This also registers the keys above with the AWS IoT service. If you browse the AWS console to IoT Core on the left hand menu select the `Secure` arrow and click `Certificates`. In the certificates grid you should be able to find your certificate by finding the `Name` that matches the returned `certificate_arn` value after the `arn:aws:iot:REGION:ACCOUNT_ID:cert/` text, an example would look something like `34185df7c5d48136fd8d3b8787830a2d4ad7dfe3833b580150af168b7eba9fe8`.

Now that we have the provisioning keys ready we need to setup the infrastructure required in the AWS IoT fleet provisioning capabilities to register and provision our IoT device. To do so we will use the [CDK](https://aws.amazon.com/cdk/) to create the required resources in your account. The CDK is an open source software development framework to define your cloud application resources using familiar programming languages. Let's take a look at the `infrastructure` directory where the CDK scripts are located. We will be using Typescript in this example and to see what resources we will be building you can look in the `lib` directory and find the `infra-stack.ts` file and open it. It will also be using a pre-provisioning hook in the `lambda` directory and finally a provisioning template in the `templates` directory. Let's launch the resources with the CDK script and walk through what was created.

To launch the CDK script we are going to go into the `infrastructure` directory and run the command below. You will now need to get the `certificate_arn` you saved from the `create-keys-and-cert.sh` script. We will use the `deploy` command of the CDK to synthesize the script to [CloudFormation](https://aws.amazon.com/cloudformation/) that will then launch the requested resources. We will be passing in the `certificate_arn` as a parameter. When the script has been synthesized it will ask you if you wish to deploy the changes. Type `y` and hit enter.

``` bash
    npm install
    npm run build
    cdk deploy --parameters certificateArn=arn:aws:iot:REGION:ACCOUNT_ID:cert/XXX

    ┌───┬─────────────────────────────────────────┬────────────────────────────────────────────────────────────────────────────────┐
    │   │ Resource                                │ Managed Policy ARN                                                             │
    ├───┼─────────────────────────────────────────┼────────────────────────────────────────────────────────────────────────────────┤
    │ + │ ${PreProvisionHookFunction/ServiceRole} │ arn:${AWS::Partition}:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole │
    ├───┼─────────────────────────────────────────┼────────────────────────────────────────────────────────────────────────────────┤
    │ + │ ${iotProvisioningHookRole}              │ arn:${AWS::Partition}:iam::aws:policy/service-role/AWSIoTThingsRegistration    │
    │ + │ ${iotProvisioningHookRole}              │ arn:${AWS::Partition}:iam::aws:policy/service-role/AWSIoTLogging               │
    │ + │ ${iotProvisioningHookRole}              │ arn:${AWS::Partition}:iam::aws:policy/AmazonS3ReadOnlyAccess                   │
    │ + │ ${iotProvisioningHookRole}              │ arn:${AWS::Partition}:iam::aws:policy/service-role/AWSIoTRuleActions           │
    └───┴─────────────────────────────────────────┴────────────────────────────────────────────────────────────────────────────────┘
    (NOTE: There may be security-related changes not in this list. See https://github.com/aws/aws-cdk/issues/1299)

    Do you wish to deploy these changes (y/n)?
``` 

Now that the script has completed, let's take a look at what was created.

**[Fleet Provisioning Template](https://docs.aws.amazon.com/iot/latest/developerguide/provision-template.html)**
A provisioning template is a JSON document that uses parameters to describe the resources your device must use to interact with AWS IoT. A template contains two sections: Parameters and Resources. There are two types of provisioning templates in AWS IoT. One is used for just-in-time provisioning (JITP) and bulk registration and the second is used for fleet provisioning. 

**[Provisioning Policy](https://docs.aws.amazon.com/iot/latest/developerguide/iot-policies.html)**
Policies define which operations or resources a device or user can access. AWS IoT policies grant or deny access to AWS IoT resources such as things, thing shadows, and MQTT topics. A device or user can invoke AWS IoT operations only if they are granted the appropriate permissions.

Policies give permissions to AWS IoT clients regardless of the authentication mechanism they use to connect to AWS IoT. To control which resources a device can access, attach one or more AWS IoT policies to the certificate associated with the device.

**[Pre-provisioning hook (Lambda)](https://docs.aws.amazon.com/iot/latest/developerguide/pre-provisioning-hook.html)**
When using AWS IoT fleet provisioning, you can set up a Lambda function to validate parameters passed from the device before allowing the device to be provisioned. This Lambda function must exist in your account before you provision a device because it's called every time a device sends a request through RegisterThing. For devices to be provisioned, your Lambda function must accept the input object and return the output object described in this section. The provisioning proceeds only if the Lambda function returns an object with `"allowProvisioning": True`.

**Pre-Provisioning Role (Lambda Role)**
This is the IAM Role that is associated to the Pre-Provisioning hook Lambda function. This describes what actions the Lambda function can take.

**Provisioning Role**
This is the IAM Role that is associated to the Provisioning template. This describes what actions the template can take.

At this point all the infrastructure required to bootstrap and provision your device has been created. The next steps will be to build the device software. We will be using Go for this example, so have some knowledge of how to create a Go binary and executing will be required. The steps below show building the binary and how to execute at the root directory.

``` bash
go build -o iot_device main.go
```

Now lets create the configuration file required to successfully boostrap your device. Copy the `example-config.yaml` file into your own named file. The pre-provisioning hook has a requirement that the serial number needs to start with `297468`. If your serial number does not start with that the provisioning will be rejected. We also need to supply the AWS IoT Endpoint that will be used to connect to bootstrap the device. You can get the endpoint by running the command below.

``` bash
aws iot describe-endpoint --endpoint-type iot:Data-ATS
```

The results will look something like below:

``` bash
{
    "endpointAddress": "xxxxxxxxx-ats.iot.REGION.amazonaws.com"
}
```

Copy the `endpointAddress` into the `endpoint` attribute in the config file. Based on the provisioning template, your IoT `ThingName` will be `fleety_SERIALNUMBER`. For connectivity after the initial bootstrap it will use the provided cert and key for the device in the `certs` directory that matches the `ThingName`.

You should now be able to execute the binary created in the root directory `iot_device`. Currently there is only one command to execute which is the `bootstrap` command and it takes a configuration file in order to use the certificates you created earlier in the `certs` directory for bootstrapping.

``` bash
./iot_device bootstrap --author "Wilford Brimley" --config .simple-go-iot-device.yaml
```
