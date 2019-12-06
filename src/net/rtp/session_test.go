package rtp

import (
	"testing"
)

func TestCloseSession(t *testing.T) {
	session := createRecvSession()
	recv := session.CreateDataReceiveChan()
	sendTestPacket(session)

	_, ok := <-recv

	if !ok {
		t.Logf("Did not receive any data")
		t.FailNow()
	}

	sendTestPacket(session)
	session.RemoveDataReceiveChan()
	assertChannelWasClosed(t, recv)
}

func assertChannelWasClosed(t *testing.T, recv DataReceiveChan) {
	select {
	case _, ok := <-recv:
		if ok {
			t.Logf("Received extra data after closing DataReceiveChan")
			t.FailNow()
		}
	default:
		t.Logf("DataReceiveChan was not closed after RemoveDataReceiveChan was called")
		t.FailNow()
	}
}

func createRecvSession() *Session {
	// for variable definitions see receive_test.go
	initSessions()
	return rsRecv
}

func sendTestPacket(*Session) {
	payload := make([]byte, 160)

	rp := rsRecv.NewDataPacket(160)
	rp.SetPayload(payload)

	// Feed into receiver session, then check if packet was processed correctly
	rsRecv.OnRecvData(rp)
}
