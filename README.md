# RTIO-DEVICE-SDK-GO

RTIO-DEVICE-SDK-GO 是设备端对接RTIO服务得Golang SDK，帮助开发者快速对接RTIO服务。

## 开始使用

### 创建项目

```sh
mkdir ~/hello-rtio
cd ~/hello-rtio
go mod init hello-rtio
```

### 添加依赖

```sh  
go get github.com/rtio-devices-sdk-go/rtio
```

### 编写代码

添加文件 `main.go`，内容如下：

```go
package main

import (
    "context"
    "fmt"
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

    session.RegisterPostHandler(0xb70a47db, func(req []byte) ([]byte, error) {
        log.Printf("%s", string(req))
        return []byte("world"), nil

    })
    session.Serve(context.Background())

    // do other things
    time.Sleep(time.Minute * 30)
}
```

## 运行

```sh
go run main.go
```

## 透过RTIO服务，使用HTTP请求设备上的服务

```sh
$ curl -X POST http://127.0.0.1:17317/cfa09baa-4913-4ad7-a936-3e26f9671b10/post_handler -d '{"uri":"/hello","id":
12667,"data":"aGVsbG8="}'
{"id":12667, "code":"CODE_OK", "data":"d29ybGQ="}
```

其中请求的`data` 是的base64编码，解码后为 `hello`，通过以下命令可以编码：

```sh
$ echo -n "hello" | base64
aGVsbG8=
```

其中响应的`data` 是的base64编码，解码后为 `world`，通过以下命令可以解码：

```sh
$ echo -n "d29ybGQ=" | base64 -d
world
```

## API列表

```go
// 连接到RTIO服务 
func devicesession.Connect(ctx context.Context, deviceID, deviceSecret, serverAddr string) (*DeviceSession, error)

// 注册Get请求处理函数
(s *DeviceSession) RegisterGetHandler(uri uint32, handler func(req []byte) ([]byte, error)) error
// 注册Post请求处理函数 
func (s *DeviceSession) RegisterPostHandler(uri uint32, handler func(req []byte) ([]byte, error)) error
// 注册ObGet请求处理函数(客户端观察者模式 observe get)
 (s *DeviceSession) RegisterObGetHandler(uri uint32, handler func(ctx context.Context, req []byte) (<-chan []byte, error)) error

// 发送Get请求到RTIO代理的设备服务
func (s *DeviceSession) Get(uri uint32, req []byte) ([]byte, error)
// 发送Get请求到RTIO代理的设备服务，带context
func (s *DeviceSession) GetWithContext(ctx context.Context, uri uint32, req []byte) ([]byte, error)
// 发送Post请求到RTIO代理的设备服务
func (s *DeviceSession) Post(uri uint32, req []byte) ([]byte, error)
// 发送Post请求到RTIO代理的设备服务，带context
func (s *DeviceSession) PostWithContext(ctx context.Context, uri uint32, req []byte) ([]byte, error)

 // 心跳间隔设置
func (s *DeviceSession) SetHeartbeatSeconds(n uint16) 

 // Trace级别设置
func rtio.SetTraceLevel(level uint32) 

```
