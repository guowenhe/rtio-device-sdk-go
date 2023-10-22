# RTIO-DEVICE-SDK-GO

RTIO-DEVICE-SDK-GO是设备端对接RTIO服务的Golang SDK，帮助开发者快速对接RTIO服务，同时实现设备端功能。

## 开始使用

### 创建项目

```sh
mkdir ~/hello-rtio
cd ~/hello-rtio
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

确定本地RTIO服务已经启动，运行下面命令，以链接到RTIO服务：

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
$ curl -X POST http://127.0.0.1:17317/cfa09baa-4913-4ad7-a936-3e26f9671b10/post_handler -d '{"uri":"/greeter","id
":12667,"data":"aGVsbG8="}'
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
