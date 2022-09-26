import boto3, json

iot_aws = boto3.client('iot')

def on_event(event, context):
    print(event)
    request_type = event["RequestType"]
    if request_type == "Create":
        return on_create(event)
    if request_type == "Update":
        return on_update(event)
    if request_type == "Delete":
        return on_delete(event)
    raise Exception("Invalid request type: %s" % request_type)

def on_create(event):
    props = event["ResourceProperties"]
    group_name = props['thingGroupName']

    response = iot_aws.create_thing_group(
        thingGroupName=group_name
    )
    output = {'Status': 'Created'}
    return {"Data": output}

def on_update(event):
    output = {'Status': 'Updated'}
    return {"Data": output}

def on_delete(event):
    props = event["ResourceProperties"]
    group_name = props['thingGroupName']

    response = iot_aws.delete_thing_group(
        thingGroupName=group_name
    )
    output = {'Status': 'success'}
    return {"Data": output}
    
def is_complete(event, context):
    request_type = event["RequestType"]
    if request_type == 'Delete': return { 'IsComplete': True }
    return { 'IsComplete': True }