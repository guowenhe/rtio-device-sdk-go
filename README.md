# RTIO-DEVICE-SDK-GO

> **Note:**  
> This project has been moved to: <https://github.com/mkrainbow/>  
> The new RTIO project URL is: <https://github.com/mkrainbow/rtio>  
> The new RTIO API is more concise, such as registering handler functions for URIs without needing to calculate the hash first. It also adds new features, such as support for TLS and JWT authentication. Additionally, the new version does not follow the naming conventions of the current version.    
> The device SDK has been updated to include a [C SDK](https://github.com/mkrainbow/rtio-device-sdk-c), which can run on constrained devices. The [GO SDK](https://github.com/mkrainbow/rtio-device-sdk-go) has also been updated.  
> Thank you for following this project!  
>   
> **注意：**  
> 该项目已迁移至：<https://github.com/mkrainbow/>  
> 新版RTIO地址为：<https://github.com/mkrainbow/rtio>  
> 新版RTIO API更为简洁，比如给URI注册处理函数，无需先计算哈希值；同时增加了新特性，比如支持TLS和JWT验证。另外，新版本没有延续当前版面的命名方式。  
> 设备SDK，增加了[C SDK](https://github.com/mkrainbow/rtio-device-sdk-c)，可运行于受限设备。更新了[GO SDK](https://github.com/mkrainbow/rtio-device-sdk-go)。  
> 感谢您的关注！


设备也是服务资源的提供者，RTIO采取REST-Like模型，使设备端开发具有和WEB服务开发相似的体验和高效。

RTIO-DEVICE-SDK-GO为Golang版SDK，用于连接RTIO服务，帮助设备端注册Handler（处理来自外部请求），并提供发送请求到RTIO代理的后端服务的方法。

## 开始使用

### 创建项目

```sh
mkdir ./hello-rtio
cd ./hello-rtio
go mod init hello-rtio
```

### 添加依赖

```sh  
go get github.com/guowenhe/rtio-device-sdk-go/rtio
```

### 编写代码

添加文件 `main.go`，内容如下：

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/guowenhe/rtio-device-sdk-go/rtio/devicesession"
)

func main() {
    serverAddr := "localhost:17017"
    deviceID := "cfa09baa-4913-4ad7-a936-3e26f9671b10"
    deviceSecret := "mb6bgso4EChvyzA05thF9+He"
    session, err := devicesession.Connect(context.Background(), deviceID, deviceSecret, serverAddr)
    if err != nil {
        log.Println(err)
        return
    }
    // URI: /greeter, CRC: 0xe5dcc140
    session.RegisterPostHandler(0xe5dcc140, func(req []byte) ([]byte, error) {
        log.Printf("received [%s] and reply [world]", string(req))
        return []byte("world"), nil

    })
    session.Serve(context.Background())

    // do other things
    time.Sleep(time.Minute * 30)
}
```

其中, 设备端URI `/greeter`对应的CRC为`0xe5dcc140`，可通过下面命令计算：

```sh
$ rtio-urihash -u "/greeter" -x
URI: /greeter, CRC: 0xe5dcc140
```

`rtio-urihash`命令安装：

```sh
go get github.com/guowenhe/rtio-urihash
go install github.com/guowenhe/rtio-urihash
$ rtio-urihash -h
Usage of rtio-urihash:
  -u string
        uri string, example: /uri/example
  -x    display digest with hex
```

## 运行

确定本地RTIO服务已经启动，可参考[RTIO编译和运行](https://github.com/guowenhe/rtio#%E7%BC%96%E8%AF%91%E5%92%8C%E8%BF%90%E8%A1%8C)。运行下面命令，以链接到RTIO服务：

```sh
go run main.go
```

## 请求设备

通过RTIO服务，请求设备端的`/greeter`接口，请求的`data` 为base64编码，可通过下面命令编码：

```sh
$ echo -n "hello" | base64
aGVsbG8=
```

通过curl发送Http请求：

```sh
$ curl -X POST http://127.0.0.1:17317/cfa09baa-4913-4ad7-a936-3e26f9671b10/post_handler -d '{"uri":"/greeter","id":12667,"data":"aGVsbG8="}'
{"id":12667, "code":"CODE_OK", "data":"d29ybGQ="}
```

其中响应的`data` 为base64编码，可通过下面命令解码：

```sh
$ echo -n "d29ybGQ=" | base64 -d
world
```

设备端输出：

```sh
$ go run main.go
2023/10/22 09:05:43 received [hello] and reply [world]
```

## API列表

```go
// 连接到RTIO服务 
func devicesession.Connect(ctx context.Context, deviceID, deviceSecret, serverAddr string) (*DeviceSession, error)

// 注册Get请求处理函数
(s *DeviceSession) RegisterGetHandler(uri uint32, handler func(req []byte) ([]byte, error)) error
// 注册Post请求处理函数 
func (s *DeviceSession) RegisterPostHandler(uri uint32, handler func(req []byte) ([]byte, error)) error
// 注册ObGet请求处理函数(客户端观察者模式 Observe-Get)
 (s *DeviceSession) RegisterObGetHandler(uri uint32, handler func(ctx context.Context, req []byte) (<-chan []byte, error)) error

// 启动服务
func (s *DeviceSession) Serve(ctx context.Context) error

// 发送Get请求到RTIO代理的设备服务（device-sevice）
func (s *DeviceSession) Get(uri uint32, Req []byte, timeout time.Duration) ([]byte, error) 
// 发送Get请求到RTIO代理的设备服务，带context
func (s *DeviceSession) GetWithContext(ctx context.Context, uri uint32, req []byte) ([]byte, error)
// 发送Post请求到RTIO代理的设备服务（device-sevice）
func (s *DeviceSession) Post(uri uint32, Req []byte, timeout time.Duration) ([]byte, error) 
// 发送Post请求到RTIO代理的设备服务，带context
func (s *DeviceSession) PostWithContext(ctx context.Context, uri uint32, req []byte) ([]byte, error)

 // 设置心跳间隔，默认300秒
func (s *DeviceSession) SetHeartbeatSeconds(n uint16) 

 // Trace级别设置，默认为1（0-不打印日志，1-关键信息，2-全部信息）
func rtio.SetTraceLevel(level uint32) 

```
