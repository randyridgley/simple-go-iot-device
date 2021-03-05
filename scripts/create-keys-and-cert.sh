#!/bin/bash

# Creates a 2048-bit RSA key pair and issues an X.509 certificate using the issued public key.
echo "Creating the keys and certificate"
CERTIFICATE_ARN=$(aws iot create-keys-and-certificate \
  --set-as-active \
  --certificate-pem-outfile "../certs/fleet-provisioning.certificate.pem" \
  --public-key-outfile "../certs/fleet-provisioning.public.key" \
  --private-key-outfile "../certs/fleet-provisioning.private.key" --output text | head -n1 | awk '{print $1}')
echo $CERTIFICATE_ARN