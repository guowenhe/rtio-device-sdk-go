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

package devicesession

import (
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"time"

	dp "github.com/guowenhe/rtio-device-sdk-go/rtio/pkg/deviceproto"
	ru "github.com/guowenhe/rtio-device-sdk-go/rtio/pkg/rtioutils"
	"github.com/guowenhe/rtio-device-sdk-go/rtio/pkg/timekv"
	"github.com/guowenhe/rtio-device-sdk-go/rtio/pkg/trace"
)

const (
	DEVICE_HEARTBEAT_SECONDS_DEFAULT = 300
)

var (
	ErrDataType           = errors.New("ErrDataType")
	ErrOverCapacity       = errors.New("ErrOverCapacity")
	ErrSendTimeout        = errors.New("ErrSendTimeout")
	ErrCanceled           = errors.New("ErrCanceled")
	ErrVerifyFailed       = errors.New("ErrVerifyFailed")
	ErrPingFailed         = errors.New("ErrPingFailed")
	ErrTransFunc          = errors.New("ErrTransFunc")
	ErrObservaNotMatch    = errors.New("ErrObservaNotMatch")
	ErrMethodNotMatch     = errors.New("ErrMethodNotMatch")
	ErrSendRespChannClose = errors.New("ErrSendRespChannClose")
	ErrHeaderIDNotExist   = errors.New("ErrHeaderIDNotExist")
)

type DeviceSession struct {
	deviceID           string
	deviceSecret       string
	serverAddr         string
	outgoingChan       chan []byte
	errChan            chan error
	regGetHandlerMap   map[uint32]func(req []byte) ([]byte, error)
	regPostHandlerMap  map[uint32]func(req []byte) ([]byte, error)
	regObGetHandlerMap map[uint32]func(ctx context.Context, req []byte) (<-chan []byte, error)
	sendIDStore        *timekv.TimeKV
	rollingHeaderID    atomic.Uint32 // rolling number for header id
	conn               net.Conn
	heartbeatSeconds   uint16
	reconnectTimes     uint16
}

func Connect(ctx context.Context, deviceID, deviceSecret, serverAddr string) (*DeviceSession, error) {
	conn, err := net.DialTimeout("tcp", serverAddr, time.Second*60)
	if err != nil {
		trace.Noticef("error=%v", err)
		return nil, err
	}
	session := newDeviceSession(conn, deviceID, deviceSecret, serverAddr)
	return session, nil
}
func (s *DeviceSession) SetHeartbeatSeconds(n uint16) {
	s.heartbeatSeconds = n
}
func (s *DeviceSession) Serve(ctx context.Context) {
	go s.serve(ctx, s.errChan)
	go s.recover(ctx, s.errChan)
}
func newDeviceSession(conn net.Conn, deviceId, deviceSecret, serverAddr string) *DeviceSession {
	s := &DeviceSession{
		deviceID:           deviceId,
		deviceSecret:       deviceSecret,
		serverAddr:         serverAddr,
		outgoingChan:       make(chan []byte, 10),
		errChan:            make(chan error, 1),
		sendIDStore:        timekv.NewTimeKV(time.Second * 120),
		regGetHandlerMap:   make(map[uint32]func(req []byte) ([]byte, error), 1),
		regPostHandlerMap:  make(map[uint32]func(req []byte) ([]byte, error), 1),
		regObGetHandlerMap: make(map[uint32]func(ctx context.Context, req []byte) (<-chan []byte, error), 1),
		conn:               conn,
		heartbeatSeconds:   DEVICE_HEARTBEAT_SECONDS_DEFAULT,
		reconnectTimes:     0,
	}
	s.rollingHeaderID.Store(0)

	return s
}
func (s *DeviceSession) genHeaderID() uint16 {
	return uint16(s.rollingHeaderID.Add(1))
}

func (s *DeviceSession) verify() error {
	headerID, err := ru.GenUint16ID()
	if err != nil {
		return err
	}
	req := &dp.VerifydReq{
		Header: &dp.Header{
			Version: dp.Version,
			Type:    dp.MsgType_DeviceVerifyReq,
			ID:      headerID,
		},
		CapLevel:     1,
		DeviceID:     s.deviceID,
		DeviceSecret: s.deviceSecret,
	}

	buf, err := dp.EncodeVerifyReq(req)
	if err != nil {
		return err
	}
	s.outgoingChan <- buf
	return nil
}
func (s *DeviceSession) ping() error { // heartbeat: 0 - not update, other - using heartbeat as new value
	headerID, err := ru.GenUint16ID()
	if err != nil {
		return err
	}
	req := &dp.PingReq{
		Header: &dp.Header{
			Version: dp.Version,
			Type:    dp.MsgType_DevicePingReq,
			ID:      headerID,
		},
		Timeout: 0,
	}

	buf, err := dp.EncodePingReq(req)
	if err != nil {
		return err
	}
	s.outgoingChan <- buf
	return nil
}
func (s *DeviceSession) pingRemoteSet(timeout uint16) error { // timeout [30, 43200]
	headerID, err := ru.GenUint16ID()
	if err != nil {
		return err
	}
	req := &dp.PingReq{
		Header: &dp.Header{
			Version: dp.Version,
			Type:    dp.MsgType_DevicePingReq,
			ID:      headerID,
		},
		Timeout: timeout,
	}

	buf, err := dp.EncodePingReq(req)
	if err != nil {
		return err
	}
	s.outgoingChan <- buf
	return nil
}

// interval : [2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, 8192, 16384, 32768 ...] secodes
// level    :  0, 1, 2,  3,  4,  5,   6,   7,   8,    9,   10,   11,   12,    13,    14
// recommend: set maxlevel=8 (max retry intervel is 8.5 minite)
func (s *DeviceSession) getReconnectInterval(maxLevel uint16) time.Duration {
	n := maxLevel
	if s.reconnectTimes < n {
		n = s.reconnectTimes
	}
	return 2 << n
}
func (s *DeviceSession) reconnect(ctx context.Context, serverAddr string, errChan chan<- error) uint16 {
	s.reconnectTimes++
	conn, err := net.DialTimeout("tcp", serverAddr, time.Second*60)
	if err != nil {
		trace.Printf("reconnect, deviceid=%s, retrytimes=%d, error=%v", s.deviceID, s.reconnectTimes, err)
		errChan <- err
		return s.reconnectTimes
	}
	s.reconnectTimes = 0 // connect sucess and reset to 0
	s.conn = conn
	trace.Noticef("reconnect server success, deviceid=%s, retrytimes=%d", s.deviceID, s.reconnectTimes)
	go s.serve(ctx, errChan)
	return s.reconnectTimes
}

func (s *DeviceSession) sendCoReq(headerID uint16, method dp.Method, uri uint32, data []byte) (<-chan []byte, error) {
	req := &dp.CoReq{
		HeaderID: headerID,
		Method:   method,
		URI:      uri,
		Data:     data,
	}

	trace.Printf("sendCoReq, headerid=%d, uri=%d", headerID, uri)
	buf := make([]byte, int(dp.HeaderLen+dp.HeaderLen_CoReq)+len(data))
	if err := dp.EncodeCoReq_OverDeviceSendReq(req, buf); err != nil {
		trace.Printf("sendCoReq, headerid=%d, error=%v", headerID, err)
		return nil, err
	}
	respChan := make(chan []byte, 1)
	s.sendIDStore.Set(timekv.Key(headerID), &timekv.Value{C: respChan})
	s.outgoingChan <- buf
	return respChan, nil
}
func (s *DeviceSession) receiveCoResp(headerID uint16, respChan <-chan []byte, timeout time.Duration) (dp.StatusCode, []byte, error) {
	defer s.sendIDStore.Del(timekv.Key(headerID))
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case sendRespBody, ok := <-respChan:
		if !ok {
			trace.Printf("receiveCoResp, headerid=%d, error=%v", headerID, ErrSendRespChannClose)
			return dp.StatusCode_Unknown, nil, ErrSendRespChannClose
		}
		coResp, err := dp.DecodeCoResp(headerID, sendRespBody)
		if err != nil {
			trace.Printf("receiveCoResp, headerid=%d, error=%v", headerID, err)
			return dp.StatusCode_Unknown, nil, err
		}
		if coResp.Method != dp.Method_ConstrainedGet && coResp.Method != dp.Method_ConstrainedPost {
			trace.Printf("receiveCoResp, headerid=%d, error=%v", headerID, ErrMethodNotMatch)
			return dp.StatusCode_InternalServerError, nil, ErrMethodNotMatch
		}
		trace.Printf("receiveCoResp, headerid=%d, status=%s", coResp.HeaderID, coResp.Code.String())

		return coResp.Code, coResp.Data, nil
	case <-t.C:
		trace.Printf("receiveCoResp, error=%v", ErrSendTimeout)
		return dp.StatusCode_Unknown, nil, ErrSendTimeout
	}
}

func (s *DeviceSession) receiveCoRespWithContext(ctx context.Context, headerID uint16, respChan <-chan []byte) (dp.StatusCode, []byte, error) {
	defer s.sendIDStore.Del(timekv.Key(headerID))
	select {
	case sendRespBody, ok := <-respChan:
		if !ok {
			trace.Printf("receiveCoRespWithContext, headerid=%d, error=%v", headerID, ErrSendRespChannClose)
			return dp.StatusCode_Unknown, nil, ErrSendRespChannClose
		}
		coResp, err := dp.DecodeCoResp(headerID, sendRespBody)
		if err != nil {
			trace.Printf("receiveCoRespWithContext, headerid=%d, error=%v", headerID, err)

			return dp.StatusCode_Unknown, nil, err
		}
		if coResp.Method != dp.Method_ConstrainedGet && coResp.Method != dp.Method_ConstrainedPost {
			trace.Printf("receiveCoRespWithContext, headerid=%d, error=%d", headerID, ErrMethodNotMatch)
			return dp.StatusCode_InternalServerError, nil, ErrMethodNotMatch
		}
		trace.Printf("WithContext, headerid=%d, status=%s", coResp.HeaderID, coResp.Code.String())
		return coResp.Code, coResp.Data, nil
	case <-ctx.Done():
		trace.Printf("WithContext context done")
		return dp.StatusCode_Unknown, nil, ErrCanceled
	}
}

func (s *DeviceSession) sendObNotifyReq(obid uint16, headerID uint16, data []byte) (<-chan []byte, error) {
	req := &dp.ObGetNotifyReq{
		HeaderID: headerID,
		ObID:     obid,
		Method:   dp.Method_ObservedGet,
		Code:     dp.StatusCode_Continue,
		Data:     data,
	}
	trace.Printf("sendObNotifyReq, headerid=%d, obid=%d", headerID, obid)

	buf := make([]byte, int(dp.HeaderLen+dp.HeaderLen_ObGetNotifyReq)+len(data))
	if err := dp.EncodeObGetNotifyReq_OverDeviceSendReq(req, buf); err != nil {
		return nil, err
	}
	respChan := make(chan []byte, 1)
	s.sendIDStore.Set(timekv.Key(headerID), &timekv.Value{C: respChan})
	s.outgoingChan <- buf
	return respChan, nil
}
func (s *DeviceSession) sendObNotifyReqTerminate(obid uint16, headerID uint16) (<-chan []byte, error) {
	req := &dp.ObGetNotifyReq{
		HeaderID: headerID,
		ObID:     obid,
		Method:   dp.Method_ObservedGet,
		Code:     dp.StatusCode_Terminate,
	}
	trace.Printf("sendObNotifyReqTerminate, headerid=%d, obid=%d", headerID, obid)

	buf := make([]byte, int(dp.HeaderLen+dp.HeaderLen_ObGetNotifyReq))
	if err := dp.EncodeObGetNotifyReq_OverDeviceSendReq(req, buf); err != nil {
		return nil, err
	}
	respChan := make(chan []byte, 1)
	s.sendIDStore.Set(timekv.Key(headerID), &timekv.Value{C: respChan})
	s.outgoingChan <- buf
	return respChan, nil
}
func (s *DeviceSession) receiveObNotifyResp(obID, headerID uint16, respChan <-chan []byte, timeout time.Duration) (dp.StatusCode, error) {
	defer s.sendIDStore.Del(timekv.Key(headerID))
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case sendRespBody, ok := <-respChan:
		if !ok {
			trace.Printf("receiveObNotifyResp, headerid=%d, error=%d", headerID, ErrSendRespChannClose)
			return dp.StatusCode_Unknown, ErrSendRespChannClose
		}
		obEstabResp, err := dp.DecodeObGetNotifyResp(headerID, sendRespBody)
		if err != nil {
			trace.Printf("receiveObNotifyResp, headerid=%d, obid=%d, error=%v", headerID, obID, err)
			return dp.StatusCode_Unknown, err
		}
		if obEstabResp.ObID != obID {
			trace.Printf("receiveObNotifyResp, headerid=%d, obid=%d, error=%v", headerID, obID, ErrObservaNotMatch)
			return dp.StatusCode_Unknown, ErrObservaNotMatch
		}
		if obEstabResp.Method != dp.Method_ObservedGet {
			trace.Printf("receiveObNotifyResp, headerid=%d, obid=%d, error=%v", headerID, obID, dp.StatusCode_MethodNotAllowed)
			return dp.StatusCode_MethodNotAllowed, nil
		}
		return obEstabResp.Code, nil
	case <-t.C:
		trace.Printf("receiveObNotifyResp, headerid=%d, obid=%d, error=%v", headerID, obID, ErrSendTimeout)
		return dp.StatusCode_Unknown, ErrSendTimeout
	}
}
func (s *DeviceSession) observe(obID uint16, cancal context.CancelFunc, notifyReqChan <-chan []byte) {

	trace.Printf("observe, obid=%d", obID)

	defer func() {
		trace.Printf("observe exit, obid=%d", obID)
	}()
	defer cancal()

	for {
		data, ok := <-notifyReqChan
		headerID := s.genHeaderID()
		if ok {
			notfyRespChan, err := s.sendObNotifyReq(obID, headerID, data)
			if err != nil {
				trace.Printf("observe, obid=%d, error=%v", obID, err)
				return
			}
			statusCode, err := s.receiveObNotifyResp(obID, headerID, notfyRespChan, time.Second*20)
			if err != nil {
				trace.Printf("observe, obid=%d, error=%v", obID, err)
				return
			}
			if dp.StatusCode_Continue != statusCode {
				trace.Printf("observe terminiate, obid=%d", obID)
				return
			}
		} else {
			trace.Printf("observe notify req channel closed, obid=%d", obID)
			notfyRespChan, err := s.sendObNotifyReqTerminate(obID, headerID)
			if err != nil {
				trace.Printf("observe, obid=%d, error=%v", obID, err)
				return
			}
			_, err = s.receiveObNotifyResp(obID, headerID, notfyRespChan, time.Second*20)
			if err != nil {
				trace.Printf("observe, obid=%d, error=%v", obID, err)
				return
			}
			return
		}
	}
}

func (s *DeviceSession) obGetReqHandler(header *dp.Header, reqBuf []byte) error {
	req, err := dp.DecodeObGetEstabReq(header.ID, reqBuf)
	if err != nil {
		trace.Printf("obGetReqHandler, obid=%d, error=%v", req.ObID, err)
		return err
	}

	resp := &dp.ObGetEstabResp{
		HeaderID: req.HeaderID,
		Method:   req.Method,
		ObID:     req.ObID,
	}
	handler, ok := s.regObGetHandlerMap[req.URI]
	respBuf := make([]byte, dp.HeaderLen+dp.HeaderLen_ObGetEstabResp)
	if !ok {
		trace.Printf("obGetReqHandler uri not found, obid=%d, uri=%d", req.ObID, req.URI)
		resp.Code = dp.StatusCode_NotFount
		if err := dp.EncodeObGetEstabResp_OverServerSendResp(resp, respBuf); err != nil {
			trace.Printf("obGetReqHandler, obid=%d, error=%v", req.ObID, err)
			return err
		}
	} else {
		ctx, cancle := context.WithCancel(context.Background())
		respChan, err := handler(ctx, req.Data)
		if err != nil {
			resp.Code = dp.StatusCode_InternalServerError
			cancle()
			return err
		}
		resp.Code = dp.StatusCode_Continue
		if err := dp.EncodeObGetEstabResp_OverServerSendResp(resp, respBuf); err != nil {
			trace.Printf("obGetReqHandler, obid=%d, error=%v", req.ObID, err)
			cancle()
			return err
		}
		go s.observe(req.ObID, cancle, respChan)
	}
	s.outgoingChan <- respBuf
	return nil
}

func (s *DeviceSession) coGetReqHandler(header *dp.Header, reqBuf []byte) error {
	req, err := dp.DecodeCoReq(header.ID, reqBuf)
	if err != nil {
		trace.Printf("coGetReqHandler, headerid=%d, error=%v", req.HeaderID, err)
		return err
	}
	resp := &dp.CoResp{
		HeaderID: req.HeaderID,
		Method:   req.Method,
	}
	handler, ok := s.regGetHandlerMap[req.URI]

	if !ok {
		trace.Printf("coGetReqHandler uri not found, headerid=%d, uri=%d", req.HeaderID, req.URI)
		resp.Code = dp.StatusCode_NotFount
		buf := make([]byte, int(dp.HeaderLen+dp.HeaderLen_CoResp))
		if err := dp.EncodeCoResp_OverServerSendResp(resp, buf); err != nil {
			trace.Printf("coGetReqHandler, headerid=%d, error=%v", req.HeaderID, err)
			return err
		}
		s.outgoingChan <- buf
	} else {
		data, err := handler(req.Data)
		if err != nil {
			resp.Code = dp.StatusCode_InternalServerError
			return err
		}
		resp.Code = dp.StatusCode_OK
		resp.Data = data
		buf := make([]byte, int(dp.HeaderLen+dp.HeaderLen_CoResp)+len(resp.Data))
		if err := dp.EncodeCoResp_OverServerSendResp(resp, buf); err != nil {
			trace.Printf("coGetReqHandler, headerid=%d, error=%v", req.HeaderID, err)
			return err
		}
		s.outgoingChan <- buf
	}
	return nil
}

func (s *DeviceSession) coPostReqHandler(header *dp.Header, reqBuf []byte) error {
	req, err := dp.DecodeCoReq(header.ID, reqBuf)
	if err != nil {
		trace.Printf("coPostReqHandler, headerid=%d, error=%v", req.HeaderID, err)
		return err
	}
	resp := &dp.CoResp{
		HeaderID: req.HeaderID,
		Method:   req.Method,
	}
	handler, ok := s.regPostHandlerMap[req.URI]

	if !ok {
		trace.Printf("coPostReqHandler uri not found, headerid=%d, uri=%d", req.HeaderID, req.URI)
		resp.Code = dp.StatusCode_NotFount
		buf := make([]byte, int(dp.HeaderLen+dp.HeaderLen_CoResp))
		if err := dp.EncodeCoResp_OverServerSendResp(resp, buf); err != nil {
			trace.Printf("coPostReqHandler, headerid=%d, error=%v", req.HeaderID, err)
			return err
		}
		s.outgoingChan <- buf
	} else {
		data, err := handler(req.Data)
		if err != nil {
			resp.Code = dp.StatusCode_InternalServerError
			return err
		}
		resp.Code = dp.StatusCode_OK
		resp.Data = data
		buf := make([]byte, int(dp.HeaderLen+dp.HeaderLen_CoResp)+len(resp.Data))
		if err := dp.EncodeCoResp_OverServerSendResp(resp, buf); err != nil {
			trace.Printf("coPostReqHandler, headerid=%d, error=%v", req.HeaderID, err)
			return err
		}
		s.outgoingChan <- buf
	}
	return nil
}

func (s *DeviceSession) serverSendRequest(header *dp.Header) error {

	bodyBuf := make([]byte, header.BodyLen)
	readLen, err := io.ReadFull(s.conn, bodyBuf)
	if err != nil {
		trace.Printf("serverSendRequest, readLen=%d, error=%v", readLen, err)
		return err
	}

	method, err := dp.DecodeMethod(bodyBuf)
	if err != nil {
		trace.Printf("serverSendRequest, error=%v", err)
		return err
	}

	switch method {
	case dp.Method_ObservedGet:
		err := s.obGetReqHandler(header, bodyBuf)
		if err != nil {
			trace.Printf("serverSendRequest, error=%v", err)
			return err
		}
	case dp.Method_ConstrainedGet:
		err := s.coGetReqHandler(header, bodyBuf)
		if err != nil {
			trace.Printf("serverSendRequest, error=%v", err)
			return err
		}
	case dp.Method_ConstrainedPost:
		err := s.coPostReqHandler(header, bodyBuf)
		if err != nil {
			trace.Printf("serverSendRequest, error=%v", err)
			return err
		}
	}
	return nil
}

func (s *DeviceSession) deviceSendRespone(header *dp.Header) error {
	value, ok := s.sendIDStore.Get(timekv.Key(header.ID))

	if !ok {
		trace.Printf("deviceSendRespone, headerid=%d", header.ID)
		return ErrHeaderIDNotExist
	}
	bodyBuf := make([]byte, header.BodyLen)
	readLen, err := io.ReadFull(s.conn, bodyBuf)
	if err != nil {
		trace.Printf("deviceSendRespone, readLen=%d, error=%v", readLen, err)
		return err
	}

	sendResp, err := dp.DecodeSendRespBody(header, bodyBuf)
	if err != nil {
		trace.Printf("deviceSendRespone, error=%v", err)
		return err
	}
	value.C <- sendResp.Body
	return nil
}

func (s *DeviceSession) tcpIncomming(ctx context.Context, errChan chan<- error) {
	defer func() {
		trace.Printf("tcpIncomming exit")
	}()

	for {
		select {
		case <-ctx.Done():
			trace.Printf("tcpIncomming context done")
			return
		default:
			headerBuf := make([]byte, dp.HeaderLen)
			readLen, err := io.ReadFull(s.conn, headerBuf)
			trace.Printf("tcpIncomming read header, readLen=%d", readLen)
			if err != nil {
				errChan <- err
				return
			}
			header, err := dp.DecodeHeader(headerBuf)
			if err != nil {
				errChan <- dp.ErrDecode
				return
			}
			trace.Printf("tcpIncomming, headerid=%d, type=%s", header.ID, header.Type.String())
			switch header.Type {
			case dp.MsgType_DeviceVerifyResp:
				if header.Code == dp.Code_Success {
					trace.Printf("tcpIncomming verify pass")
				} else {
					trace.Printf("tcpIncomming verify fail, resp.code=%d", header.Code)
					errChan <- ErrVerifyFailed
					return
				}
			case dp.MsgType_DevicePingResp:
				if header.Code == dp.Code_Success {
					trace.Printf("tcpIncomming ping success")
				} else {
					trace.Printf("tcpIncomming ping fail, resp.code=%d", header.Code)
					errChan <- ErrPingFailed
					return
				}
			case dp.MsgType_ServerSendReq:
				err := s.serverSendRequest(header)
				if err != nil {
					trace.Printf("tcpIncomming, error=%v", err)
					errChan <- err
					return
				}
			case dp.MsgType_DeviceSendResp:
				if err := s.deviceSendRespone(header); err != nil {
					errChan <- err
					return
				}
			default:
				errChan <- ErrDataType
				return
			}

		}
	}
}

func (s *DeviceSession) tcpOutgoing(ctx context.Context, heartbeat *time.Ticker, errChan chan<- error) {
	defer func() {
		trace.Printf("tcpOutgoing exit")
	}()

	for {
		select {
		case <-ctx.Done():
			trace.Printf("tcpOutgoing context done")
			return
		case buf := <-s.outgoingChan:
			heartbeat.Reset(time.Second * time.Duration(s.heartbeatSeconds))
			if writelen, err := ru.WriteFull(s.conn, buf); err != nil {
				trace.Printf("tcpIncomming WriteFull error, writelen=%d buflen=%d, error=%v", writelen, len(buf), err)
				errChan <- err
				return
			}
		}
	}
}

func (s *DeviceSession) serve(ctx context.Context, errChan chan<- error) {
	trace.Printf("serve, deviceip=%s deviceid=%s", s.conn.LocalAddr().String(), s.deviceID)

	defer func() {
		trace.Printf("serve stop, deviceip=%s", s.conn.LocalAddr().String())
	}()

	ioCtx, ioCancel := context.WithCancel(ctx)
	defer ioCancel()

	heartbeat := time.NewTicker(time.Second * time.Duration(s.heartbeatSeconds))
	defer heartbeat.Stop()

	errNetChan := make(chan error, 2)
	go s.tcpIncomming(ioCtx, errNetChan)
	go s.tcpOutgoing(ioCtx, heartbeat, errNetChan)

	if err := s.verify(); err != nil {
		trace.Printf("serve stop, deviceip=%s, error=%v", s.conn.LocalAddr().String(), err)
		return
	}

	if s.heartbeatSeconds != DEVICE_HEARTBEAT_SECONDS_DEFAULT {
		s.pingRemoteSet(s.heartbeatSeconds)
	}

	storeTicker := time.NewTicker(time.Second * 5)
	defer storeTicker.Stop()

	for {
		select {
		case err := <-errNetChan:
			errChan <- err
			trace.Noticef("serve, device done when error, deviceip=%s, error=%v", s.conn.LocalAddr().String(), err)
			s.conn.Close()
			ioCancel()
			return
		case <-ioCtx.Done():
			trace.Printf("serve, device done when context done, deviceip=%s", s.conn.LocalAddr().String())
			s.conn.Close()
			// if err := s.conn.Close(); err != nil {
			// trace.Printf("serve, error when close conn, deviceip=%s", s.conn.LocalAddr().String())
			// }
			return
		case <-heartbeat.C:
			trace.Printf("serve, ping req, deviceip=%s", s.conn.LocalAddr().String())
			s.ping()
		case <-storeTicker.C:
			// trace.Printf("serve, deviceip=%s, tick=%s", s.conn.LocalAddr().String(), time.Now().String())
			s.sendIDStore.DelExpireKeys()
		}
	}
}

func (s *DeviceSession) recover(ctx context.Context, errChan chan error) {
	for {
		select {
		case <-ctx.Done():
			trace.Printf("recover ctx done")
			return
		case err := <-errChan:
			trace.Noticef("recover, reconnect later, error=%v", err)
			time.AfterFunc(time.Second*s.getReconnectInterval(6), func() {
				s.reconnect(ctx, s.serverAddr, errChan)
			})
		}
	}
}
