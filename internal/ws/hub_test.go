package ws

import (
	"testing"
	"time"
)

func TestHubBroadcast(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	ch := make(chan []byte, 10)
	client := &Client{hub: hub, send: ch}
	hub.Register(client)

	time.Sleep(10 * time.Millisecond)

	hub.Broadcast([]byte(`{"type":"progress","percent":50}`))
	time.Sleep(10 * time.Millisecond)

	select {
	case msg := <-ch:
		if string(msg) != `{"type":"progress","percent":50}` {
			t.Errorf("got %q", string(msg))
		}
	default:
		t.Error("no message received")
	}
}

func TestHubMultipleClients(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	ch1 := make(chan []byte, 10)
	ch2 := make(chan []byte, 10)
	client1 := &Client{hub: hub, send: ch1}
	client2 := &Client{hub: hub, send: ch2}
	hub.Register(client1)
	hub.Register(client2)

	time.Sleep(10 * time.Millisecond)

	hub.Broadcast([]byte("test"))
	time.Sleep(10 * time.Millisecond)

	select {
	case msg := <-ch1:
		if string(msg) != "test" {
			t.Errorf("client1 got %q", string(msg))
		}
	default:
		t.Error("client1 received no message")
	}

	select {
	case msg := <-ch2:
		if string(msg) != "test" {
			t.Errorf("client2 got %q", string(msg))
		}
	default:
		t.Error("client2 received no message")
	}
}
