/*
*
* Copyright 2023 RTIO authors.
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You may obtain a copy of the License at
*
*      http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
 */
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
