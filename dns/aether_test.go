package dns

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testTransport struct {
	publish   chan *Message
	subscribe chan *Message
}

var _ Transport = (*testTransport)(nil)

func (t *testTransport) Subscribe(ctx context.Context, channel string) Subscriber {
	t.subscribe = make(chan *Message, 1)
	t.publish = make(chan *Message, 1)
	return &testSubscriber{channel: channel, messages: t.subscribe}
}

func (t *testTransport) Publish(ctx context.Context, channel string, payload []byte) error {
	t.publish <- &Message{Payload: payload}
	var cert DNSCert
	cert.Certificate = []byte("cert")
	cert.Expires = time.Now().Add(time.Hour * 24 * 365 * 2)
	cert.PrivateKey = []byte("private")
	var response DNSResponse[DNSCert]
	response.Success = true
	response.Data = &cert
	response.MsgID = uuid.New().String()
	response.Error = ""
	response.Data = &cert
	responseBytes, err := json.Marshal(response)
	if err != nil {
		return err
	}
	t.subscribe <- &Message{Payload: responseBytes}
	return nil
}

type testSubscriber struct {
	channel  string
	messages chan *Message
}

var _ Subscriber = (*testSubscriber)(nil)

func (s *testSubscriber) Close() error {
	return nil
}

func (s *testSubscriber) Channel() <-chan *Message {
	return s.messages
}

func TestDNSAction(t *testing.T) {
	var transport testTransport

	action := &DNSAddAction{
		DNSBaseAction: DNSBaseAction{
			MsgID:  uuid.New().String(),
			Action: "add",
		},
		Name: "test",
	}

	reply, err := SendDNSAction(context.Background(), action, WithTransport(&transport), WithTimeout(time.Second))
	if err != nil {
		t.Fatalf("failed to send dns action: %v", err)
	}

	assert.NoError(t, err)
	assert.NotNil(t, reply)
}

func TestDNSCertAction(t *testing.T) {
	var transport testTransport

	action := &DNSCertAction{
		DNSBaseAction: DNSBaseAction{
			MsgID:  uuid.New().String(),
			Action: "cert",
		},
		Name: "test",
	}

	reply, err := SendDNSAction(context.Background(), action, WithTransport(&transport), WithTimeout(time.Second))
	if err != nil {
		t.Fatalf("failed to send dns cert action: %v", err)
	}

	assert.NoError(t, err)
	assert.NotNil(t, reply)
	assert.Equal(t, "cert", string(reply.Certificate))
	assert.Equal(t, "private", string(reply.PrivateKey))
	assert.True(t, reply.Expires.After(time.Now()))
	a, err := ActionFromChannel("aether:response:cert:123")
	assert.NoError(t, err)
	assert.Equal(t, "cert", a)
}

func TestWithCatalyst_AddRecord(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/aether/dns/add", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Contains(t, r.Header.Get("User-Agent"), "Agentuity")

		var req map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "test.example.com", req["name"])
		assert.Equal(t, "A", req["type"])
		assert.Equal(t, "10.0.0.1", req["value"])
		assert.Equal(t, float64(60), req["ttl"])
		assert.Equal(t, float64(300), req["expires"])

		resp := map[string]interface{}{
			"ids": []string{"record-123"},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	action := NewAddAction("test.example.com", RecordTypeA, "10.0.0.1").
		WithTTL(time.Minute).
		WithExpires(5 * time.Minute)

	result, err := SendDNSAction(context.Background(), action, WithCatalyst(server.URL, "test-token"))
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, []string{"record-123"}, result.IDs)
}

func TestWithCatalyst_AddRecordWithAllFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "srv.example.com", req["name"])
		assert.Equal(t, "SRV", req["type"])
		assert.Equal(t, "target.example.com", req["value"])
		assert.Equal(t, float64(10), req["priority"])
		assert.Equal(t, float64(20), req["weight"])
		assert.Equal(t, float64(8080), req["port"])

		resp := map[string]interface{}{
			"ids": []string{"srv-456"},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	action := NewAddAction("srv.example.com", RecordTypeSRV, "target.example.com").
		WithPriority(10).
		WithWeight(20).
		WithPort(8080)

	result, err := SendDNSAction(context.Background(), action, WithCatalyst(server.URL, "test-token"))
	require.NoError(t, err)
	assert.Equal(t, []string{"srv-456"}, result.IDs)
}

func TestWithCatalyst_DeleteRecord(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/aether/dns/delete", r.URL.Path)

		var req map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "test.example.com", req["name"])
		assert.Equal(t, []interface{}{"record-123"}, req["ids"])

		resp := map[string]interface{}{
			"deleted": true,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	action := DeleteDNSAction("test.example.com", "record-123")

	result, err := SendDNSAction(context.Background(), action, WithCatalyst(server.URL, "test-token"))
	require.NoError(t, err)
	_ = result
}

func TestWithCatalyst_DeleteRecordAllIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "test.example.com", req["name"])
		_, hasIDs := req["ids"]
		assert.False(t, hasIDs)

		resp := map[string]interface{}{
			"deleted": true,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	action := DeleteDNSAction("test.example.com")

	_, err := SendDNSAction(context.Background(), action, WithCatalyst(server.URL, "test-token"))
	require.NoError(t, err)
}

func TestWithCatalyst_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		resp := map[string]interface{}{
			"error": "missing dns:write scope",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	action := NewAddAction("test.example.com", RecordTypeA, "10.0.0.1")

	_, err := SendDNSAction(context.Background(), action, WithCatalyst(server.URL, "test-token"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing dns:write scope")
}

func TestWithCatalyst_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	action := NewAddAction("test.example.com", RecordTypeA, "10.0.0.1")

	_, err := SendDNSAction(context.Background(), action, WithCatalyst(server.URL, "test-token"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestWithCatalyst_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	action := NewAddAction("test.example.com", RecordTypeA, "10.0.0.1")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := SendDNSAction(ctx, action, WithCatalyst(server.URL, "test-token"))
	require.Error(t, err)
}

func TestWithCatalystClient_CustomClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.Header.Get("User-Agent"), "Agentuity")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"ids": []string{"123"}})
	}))
	defer server.Close()

	customClient := &http.Client{
		Timeout: 5 * time.Second,
	}

	action := NewAddAction("test.example.com", RecordTypeA, "10.0.0.1")

	result, err := SendDNSAction(context.Background(), action, WithCatalystClient(server.URL, "test-token", customClient))
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestDefaultCatalystHTTPClient_Timeout(t *testing.T) {
	assert.Equal(t, 10*time.Second, defaultCatalystHTTPClient.Timeout)
}

func TestWithCatalyst_CertActionNotSupported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called for cert action")
	}))
	defer server.Close()

	action := CertRequestDNSAction("test.example.com")

	_, err := SendDNSAction(context.Background(), action, WithCatalyst(server.URL, "test-token"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "certificate actions are not supported")
	assert.Contains(t, err.Error(), "Redis transport")
}

func TestWithCatalystClient_NilClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"ids": []string{"123"}})
	}))
	defer server.Close()

	action := NewAddAction("test.example.com", RecordTypeA, "10.0.0.1")

	result, err := SendDNSAction(context.Background(), action, WithCatalystClient(server.URL, "test-token", nil))
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, []string{"123"}, result.IDs)
}
