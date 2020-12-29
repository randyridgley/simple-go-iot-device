#!/bin/bash

# Creates a 2048-bit RSA key pair and issues an X.509 certificate using the issued public key.
echo "\nCreating the keys and certificate"
CERTIFICATE_ARN=$(aws iot create-keys-and-certificate \
  --set-as-active \
  --certificate-pem-outfile "../certs/fleet-provisioning.certificate.pem" \
  --public-key-outfile "../certs/fleet-provisioning.public.key" \
  --private-key-outfile "../certs/fleet-provisioning.private.key" | python -c 'import json,sys;obj=json.load(sys.stdin);print obj["certificateArn"]')
echo $CERTIFICATE_ARN