BOSS - Blacksmith On-demand ServiceS
====================================

`boss` is a command-line utility for interacting with
[Blacksmith][bs], in case you don't have a Cloud Foundry or
Kubernetes cluster handy.  It's great fun at demos.

## Version 2.0 Updates

This version includes significant updates to align with the latest Blacksmith API:

- **Updated API Compatibility**: Now supports Blacksmith API v2.16
- **Enhanced Error Handling**: Comprehensive error types and better error messages
- **Async Operation Support**: Proper handling of long-running operations
- **Improved Retry Logic**: Automatic retries for transient failures
- **YAML Validation**: Validates manifest and credential files
- **Streaming Logs**: Support for real-time task log streaming
- **Better Debugging**: Enhanced debug output and tracing

![boss is not in any way affiliate with THE BOSS, Bruce Springsteen](boss.jpg)

You can use it to view the Blacksmith Catalog:

```
→ boss catalog
Service     Plans                 Tags
=======     =====                 ====
mariadb     standalone            blacksmith
                                  dedicated
                                  mariadb

postgresql  small-cluster         blacksmith
            standalone            dedicated
                                  postgresql

rabbitmq    cluster               blacksmith
            dedicated             dedicated
                                  rabbitmq

redis       dedicated-cache       blacksmith
            dedicated-persistent  dedicated
                                  redis
```

... or to see what you've provisioned so far:

```
→ boss ls -l
ID                 Service   (ID)      Plan       (ID)
==                 =======   ====      ====       ====
relaxed-tesla      rabbitmq  rabbitmq  dedicated  rabbitmq-dedicated
agitated-jennings  rabbitmq  rabbitmq  dedicated  rabbitmq-dedicated
brave-khorana      rabbitmq  rabbitmq  dedicated  rabbitmq-dedicated
clever-mccarthy    rabbitmq  rabbitmq  dedicated  rabbitmq-dedicated
crazy-murdock      rabbitmq  rabbitmq  dedicated  rabbitmq-dedicated
ecstatic-yonath    rabbitmq  rabbitmq  dedicated  rabbitmq-dedicated
flamboyant-booth   rabbitmq  rabbitmq  dedicated  rabbitmq-dedicated
naughty-solomon    rabbitmq  rabbitmq  dedicated  rabbitmq-dedicated
```

You can create and delete services:

```
→ boss create rabbitmq/dedicated -f
rabbitmq/dedicated instance ecstatic-yonath created.

tailing deployment task log...
Task 10731 | 03:25:01 | Preparing deployment: Preparing deployment started
Task 10731 | 03:25:03 | Preparing deployment: Preparing deployment finished
Task 10731 | 03:25:03 | Preparing package compilation: Finding packages to compile started
Task 10731 | 03:25:03 | Preparing package compilation: Finding packages to compile finished
Task 10731 | 03:25:04 | Creating missing vms: standalone/58bdb1b3-9ff1-49c1-b7c1-badc42a8c892 (0) started
Task 10731 | 03:26:18 | Creating missing vms: standalone/58bdb1b3-9ff1-49c1-b7c1-badc42a8c892 (0) finished
Task 10731 | 03:26:19 | Updating instance: standalone/58bdb1b3-9ff1-49c1-b7c1-badc42a8c892 (0) (canary) started
Task 10731 | 03:26:49 | Updating instance: standalone/58bdb1b3-9ff1-49c1-b7c1-badc42a8c892 (0) (canary) finished

→ boss delete ecstatic-yonath
ecstatic-yonath instance deleted.
```

It can view BOSH manifests, deployment task logs, and service
credentials, too!

```
→ boss task relaxed-tesla
→ boss manifest relaxed-tesla
→ boss creds relaxed-tesla
```

## API Changes in v2.0

### Breaking Changes

- **Update Function Signature**: The `Update()` function now requires `plan` and `params` parameters:
  ```go
  // Old (v1.x)
  client.Update(instanceID, serviceID)
  
  // New (v2.0)
  client.Update(instanceID, serviceID, planID, params)
  ```

- **Create Function Signature**: The `Create()` function now accepts parameters:
  ```go
  // Old (v1.x)
  client.Create(instanceID, serviceID, planID)
  
  // New (v2.0)
  client.Create(instanceID, serviceID, planID, params)
  ```

### New Features

- **Enhanced Error Types**: Use `IsNotFound()`, `IsConflict()`, and `IsTimeout()` helper functions
- **Async Operations**: Operations now support the `accepts_incomplete=true` parameter
- **Retry Logic**: Configurable retry behavior with exponential backoff
- **YAML Validation**: Automatic validation of manifests and credentials
- **Streaming Logs**: New `StreamTask()` method for real-time log following

### Configuration

New client configuration options:

```go
client := Client{
    URL:                "https://blacksmith.example.com",
    Username:           "admin", 
    Password:           "password",
    InsecureSkipVerify: false,
    Debug:              true,
    Trace:              false,
    Timeout:            30 * time.Second,
    MaxRetries:         3,
    BrokerAPIVersion:   "2.16",
}
```

### Error Handling

Enhanced error handling with structured error types:

```go
instance, err := client.Create("my-db", "postgresql", "small", nil)
if err != nil {
    if IsNotFound(err) {
        fmt.Println("Service or plan not found")
    } else if IsConflict(err) {
        fmt.Println("Instance already exists")
    } else if IsTimeout(err) {
        fmt.Println("Operation timed out")
    } else {
        fmt.Printf("Error: %v\n", err)
    }
}
```

## Migration Guide

If you're upgrading from v1.x:

1. **Update Function Calls**: Add the new parameters to `Create()` and `Update()` calls
2. **Error Handling**: Review error handling code to use new error types
3. **Configuration**: Update client initialization if using custom timeouts or retry settings
4. **Dependencies**: Ensure you're using Go 1.25.0 or later

## Troubleshooting

### Common Issues

- **Connection Errors**: Check the Blacksmith URL and credentials
- **SSL Errors**: Use `--skip-ssl-validation` for self-signed certificates
- **Timeout Errors**: Increase the timeout or check network connectivity
- **Authentication Errors**: Verify username and password

### Debug Mode

Enable debug mode for detailed logging:

```bash
boss --debug catalog
```

Use trace mode for HTTP request/response details:

```bash
boss --trace --debug catalog
```

How Do I Contribute?
--------------------

  1. Fork this repo
  2. Create your feature branch (`git checkout -b my-new-feature`)
  3. Commit your changes (`git commit -am 'Added some feature'`)
  4. Push to the branch (`git push origin my-new-feature`)
  5. Create a new Pull Request in Github
  6. Profit!


[bs]: https://github.com/blacksmith-community/blacksmith
