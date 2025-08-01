// Copyright 2015 go-smpp authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package smpp

import (
	"fmt"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/Bar9-me/go-smpp/smpp/pdu"
	"github.com/Bar9-me/go-smpp/smpp/pdu/pdufield"
	"github.com/Bar9-me/go-smpp/smpp/pdu/pdutext"
	"github.com/Bar9-me/go-smpp/smpp/smpptest"
)

func TestShortMessage(t *testing.T) {
	s := smpptest.NewUnstartedServer()
	s.Handler = func(c smpptest.Conn, p pdu.Body) {
		switch p.Header().ID {
		case pdu.SubmitSMID:
			r := pdu.NewSubmitSMResp()
			r.Header().Seq = p.Header().Seq
			r.Fields().Set(pdufield.MessageID, "foobar")
			c.Write(r)
		default:
			smpptest.EchoHandler(c, p)
		}
	}
	s.Start()
	defer s.Close()
	tx := &Transmitter{
		Addr:        s.Addr(),
		User:        smpptest.DefaultUser,
		Passwd:      smpptest.DefaultPasswd,
		RateLimiter: rate.NewLimiter(rate.Limit(10), 1),
	}
	defer tx.Close()
	conn := <-tx.Bind()
	switch conn.Status() {
	case Connected:
	default:
		t.Fatal(conn.Error())
	}
	sm, err := tx.Submit(&ShortMessage{
		Src:      "root",
		Dst:      "foobar",
		Text:     pdutext.Raw("Lorem ipsum"),
		Validity: 10 * time.Minute,
		Register: pdufield.NoDeliveryReceipt,
	})
	if err != nil {
		t.Fatal(err)
	}
	msgid := sm.RespID()
	if msgid == "" {
		t.Fatalf("pdu does not contain msgid: %#v", sm.Resp())
	}
	if msgid != "foobar" {
		t.Fatalf("unexpected msgid: want foobar, have %q", msgid)
	}
}

func TestShortMessageWindowSize(t *testing.T) {
	s := smpptest.NewUnstartedServer()
	s.Handler = func(c smpptest.Conn, p pdu.Body) {
		time.Sleep(200 * time.Millisecond)
		r := pdu.NewSubmitSMResp()
		r.Header().Seq = p.Header().Seq
		r.Fields().Set(pdufield.MessageID, "foobar")
		c.Write(r)
	}
	s.Start()
	defer s.Close()
	tx := &Transmitter{
		Addr:        s.Addr(),
		User:        smpptest.DefaultUser,
		Passwd:      smpptest.DefaultPasswd,
		WindowSize:  2,
		RespTimeout: time.Second,
	}
	defer tx.Close()
	conn := <-tx.Bind()
	switch conn.Status() {
	case Connected:
	default:
		t.Fatal(conn.Error())
	}
	msgc := make(chan *ShortMessage, 3)
	defer close(msgc)
	errc := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func(msgc chan *ShortMessage, errc chan error) {
			m := <-msgc
			if m == nil {
				return
			}
			_, err := tx.Submit(m)
			errc <- err
		}(msgc, errc)
		msgc <- &ShortMessage{
			Src:      "root",
			Dst:      "foobar",
			Text:     pdutext.Raw("Lorem ipsum"),
			Validity: 10 * time.Minute,
			Register: pdufield.NoDeliveryReceipt,
		}
	}
	nerr := 0
	for i := 0; i < 3; i++ {
		if <-errc == ErrMaxWindowSize {
			nerr++
		}
	}
	if nerr != 1 {
		t.Fatalf("unexpected # of errors. want 1, have %d", nerr)
	}
}

func TestLongMessage(t *testing.T) {
	s := smpptest.NewUnstartedServer()
	count := 0
	s.Handler = func(c smpptest.Conn, p pdu.Body) {
		switch p.Header().ID {
		case pdu.SubmitSMID:
			r := pdu.NewSubmitSMResp()
			r.Header().Seq = p.Header().Seq
			r.Fields().Set(pdufield.MessageID, fmt.Sprintf("foobar%d", count))
			count++
			c.Write(r)
		default:
			smpptest.EchoHandler(c, p)
		}
	}
	s.Start()
	defer s.Close()
	tx := &Transmitter{
		Addr:   s.Addr(),
		User:   smpptest.DefaultUser,
		Passwd: smpptest.DefaultPasswd,
	}
	defer tx.Close()
	conn := <-tx.Bind()
	switch conn.Status() {
	case Connected:
	default:
		t.Fatal(conn.Error())
	}
	parts, err := tx.SubmitLongMsg(&ShortMessage{
		Src:      "root",
		Dst:      "foobar",
		Text:     pdutext.Raw("Lorem ipsum dolor sit amet, consectetur adipiscing elit. Nam consequat nisl enim, vel finibus neque aliquet sit amet. Interdum et malesuada fames ac ante ipsum primis in faucibus."),
		Validity: 10 * time.Minute,
		Register: pdufield.NoDeliveryReceipt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 {
		t.Fatalf("expected %d responses, but received %d", 2, len(parts))
	}
	for index, sm := range parts {
		msgid := sm.RespID()
		if msgid == "" {
			t.Fatalf("pdu does not contain msgid: %#v", sm.Resp())
		}
		if msgid != fmt.Sprintf("foobar%d", index) {
			t.Fatalf("unexpected msgid: want foobar%d, have %q", index, msgid)
		}
	}
}

func TestLongMessageAsUCS2(t *testing.T) {
	s := smpptest.NewUnstartedServer()
	var receivedMsg string
	shortMsg := "Lorem ipsum dolor sit amet, consectetur adipiscing elit. Nam consequat nisl enim, vel finibus neque aliquet sit amet. Interdum et malesuada fames ac ante ipsum primis in faucibus. ✓"
	count := 0
	s.Handler = func(c smpptest.Conn, p pdu.Body) {
		switch p.Header().ID {
		case pdu.SubmitSMID:
			r := pdu.NewSubmitSMResp()
			r.Header().Seq = p.Header().Seq
			r.Fields().Set(pdufield.MessageID, fmt.Sprintf("foobar%d", count))
			count++
			smByts := p.Fields()[pdufield.ShortMessage].Bytes()
			switch pdutext.DataCoding(p.Fields()[pdufield.DataCoding].Raw().(uint8)) {
			case pdutext.Latin1Type:
				receivedMsg = receivedMsg + string(pdutext.Latin1(smByts)[7:].Decode())
			case pdutext.UCS2Type:
				receivedMsg = receivedMsg + string(pdutext.UCS2(smByts)[7:].Decode())
			default:
				receivedMsg = receivedMsg + string(smByts[7:])
			}
			c.Write(r)
		default:
			smpptest.EchoHandler(c, p)
		}
	}
	s.Start()
	defer s.Close()
	tx := &Transmitter{
		Addr:   s.Addr(),
		User:   smpptest.DefaultUser,
		Passwd: smpptest.DefaultPasswd,
	}
	defer tx.Close()
	conn := <-tx.Bind()
	switch conn.Status() {
	case Connected:
	default:
		t.Fatal(conn.Error())
	}
	parts, err := tx.SubmitLongMsg(&ShortMessage{
		Src:      "root",
		Dst:      "foobar",
		Text:     pdutext.UCS2(shortMsg),
		Validity: 10 * time.Minute,
		Register: pdufield.NoDeliveryReceipt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 3 {
		t.Fatalf("expected %d responses, but received %d", 3, len(parts))
	}
	for index, sm := range parts {
		msgid := sm.RespID()
		if msgid == "" {
			t.Fatalf("pdu does not contain msgid: %#v", sm.Resp())
		}

		if receivedMsg != shortMsg {
			t.Fatalf("receivedMsg: %v, does not match shortMsg: %v", receivedMsg, shortMsg)
		}

		if msgid != fmt.Sprintf("foobar%d", index) {
			t.Fatalf("unexpected msgid: want foobar%d, have %q", index, msgid)
		}
	}
}

func TestQuerySM(t *testing.T) {
	s := smpptest.NewUnstartedServer()
	s.Handler = func(c smpptest.Conn, p pdu.Body) {
		r := pdu.NewQuerySMResp()
		r.Header().Seq = p.Header().Seq
		r.Fields().Set(pdufield.MessageID, p.Fields()[pdufield.MessageID])
		r.Fields().Set(pdufield.MessageState, 2)
		c.Write(r)
	}
	s.Start()
	defer s.Close()
	tx := &Transmitter{
		Addr:   s.Addr(),
		User:   smpptest.DefaultUser,
		Passwd: smpptest.DefaultPasswd,
	}
	defer tx.Close()
	conn := <-tx.Bind()
	switch conn.Status() {
	case Connected:
	default:
		t.Fatal(conn.Error())
	}
	qr, err := tx.QuerySM("root", "13", uint8(5), uint8(0))
	if err != nil {
		t.Fatal(err)
	}
	if qr.MsgID != "13" {
		t.Fatalf("unexpected msgid: want 13, have %s", qr.MsgID)
	}
	if qr.MsgState != "DELIVERED" {
		t.Fatalf("unexpected state: want DELIVERED, have %q", qr.MsgState)
	}
}

func TestSubmitMulti(t *testing.T) {
	//construct a byte array with the UnsuccessSme
	var bArray []byte
	bArray = append(bArray, byte(0x00))       // TON
	bArray = append(bArray, byte(0x00))       // NPI
	bArray = append(bArray, []byte("123")...) // Address
	bArray = append(bArray, byte(0x00))       // Error
	bArray = append(bArray, byte(0x00))       // Error
	bArray = append(bArray, byte(0x00))       // Error
	bArray = append(bArray, byte(0x11))       // Error
	bArray = append(bArray, byte(0x00))       // null terminator

	s := smpptest.NewUnstartedServer()
	s.Handler = func(c smpptest.Conn, p pdu.Body) {
		switch p.Header().ID {
		case pdu.SubmitMultiID:
			r := pdu.NewSubmitMultiResp()
			r.Header().Seq = p.Header().Seq
			r.Fields().Set(pdufield.MessageID, "foobar")
			r.Fields().Set(pdufield.NoUnsuccess, uint8(1))
			r.Fields().Set(pdufield.UnsuccessSme, bArray)
			c.Write(r)
		default:
			smpptest.EchoHandler(c, p)
		}
	}
	s.Start()
	defer s.Close()
	tx := &Transmitter{
		Addr:   s.Addr(),
		User:   smpptest.DefaultUser,
		Passwd: smpptest.DefaultPasswd,
	}
	defer tx.Close()
	conn := <-tx.Bind()
	switch conn.Status() {
	case Connected:
	default:
		t.Fatal(conn.Error())
	}
	sm, err := tx.Submit(&ShortMessage{
		Src:      "root",
		DstList:  []string{"123", "2233", "32322", "4234234"},
		DLs:      []string{"DistributionList1"},
		Text:     pdutext.Raw("Lorem ipsum"),
		Validity: 10 * time.Minute,
		Register: pdufield.NoDeliveryReceipt,
	})
	if err != nil {
		t.Fatal(err)
	}
	msgid := sm.RespID()
	if msgid == "" {
		t.Fatalf("pdu does not contain msgid: %#v", sm.Resp())
	}
	if msgid != "foobar" {
		t.Fatalf("unexpected msgid: want foobar, have %q", msgid)
	}
	noUncess, _ := sm.NumbUnsuccess()
	if noUncess != 1 {
		t.Fatalf("unexpected number of unsuccess %d", noUncess)
	}
	uncessSmes, _ := sm.UnsuccessSmes()
	if len(uncessSmes) != 1 {
		t.Fatalf("unsucess sme list should have a size of 1, has %d", len(uncessSmes))
	}
}

func TestNotConnected(t *testing.T) {
	s := smpptest.NewUnstartedServer()
	s.Handler = func(c smpptest.Conn, p pdu.Body) {
		switch p.Header().ID {
		case pdu.SubmitSMID:
			r := pdu.NewSubmitSMResp()
			r.Header().Seq = p.Header().Seq
			r.Fields().Set(pdufield.MessageID, "foobar")
			c.Write(r)
		default:
			smpptest.EchoHandler(c, p)
		}
	}
	s.Start()
	defer s.Close()
	tx := &Transmitter{
		Addr:   s.Addr(),
		User:   smpptest.DefaultUser,
		Passwd: smpptest.DefaultPasswd,
	}
	// Open connection and then close it
	conn := <-tx.Bind()
	switch conn.Status() {
	case Connected:
	default:
		t.Fatal(conn.Error())
	}
	tx.Close()
	_, err := tx.Submit(&ShortMessage{
		Src:      "root",
		Dst:      "foobar",
		Text:     pdutext.Raw("Lorem ipsum"),
		Validity: 10 * time.Minute,
		Register: pdufield.NoDeliveryReceipt,
	})
	if err != ErrNotConnected {
		t.Fatalf("Error should be not connect, got %s", err.Error())
	}

}
