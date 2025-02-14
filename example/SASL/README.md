## SASL example

Before running this example, you need to have a SASL server running. For quick start, I have provide a docker-compose file to
start a memcached server with SASL enabled.

> NOTICE: the docker-compose file is only for testing purpose. Please do not use it in production.
> And it's running on containerd runtime and Apple ARM64 architecture, so may not work on other platforms.
> please run your own server if the docker-compose file does not work for you.


```bash
nerdctl.lima compose up -d
# or using
docker-compose up -d

go run main.go
```
