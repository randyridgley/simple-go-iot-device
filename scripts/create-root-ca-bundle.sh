#!/bin/bash


# Copyright © 2020 Randy Ridgley randy.ridgley@gmail.com
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

# create-root-ca-bundle.sh
# Get the CA certificates which could be used to sign
# AWS IoT server certificates
# See also: https://docs.aws.amazon.com/iot/latest/developerguide/managing-device-certs.html

ROOT_CA_FILE=../certs/root.ca.bundle-test.pem
cp /dev/null $ROOT_CA_FILE

for ca in \
    https://www.amazontrust.com/repository/AmazonRootCA1.pem \
    https://www.amazontrust.com/repository/AmazonRootCA2.pem \
    https://www.amazontrust.com/repository/AmazonRootCA3.pem \
    https://www.amazontrust.com/repository/AmazonRootCA4.pem \
    https://www.websecurity.digicert.com/content/dam/websitesecurity/digitalassets/desktop/pdfs/roots/VeriSign-Class%203-Public-Primary-Certification-Authority-G5.pem; do

    echo "getting CA: $ca"
    wget -O - $ca >> $ROOT_CA_FILE

done

echo "Stored CA certificates in $ROOT_CA_FILE"