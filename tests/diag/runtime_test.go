package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

// // // // // // // // // //

func TestHandleGOMAXPROCS(t *testing.T) {
	original := runtime.GOMAXPROCS(0)
	target := 1
	if original == 1 && runtime.NumCPU() > 1 {
		target = 2
	}

	server := &serverObj{}
	defer server.gomaxprocs.restore()
	request := httptest.NewRequest(http.MethodPost, "/runtime/gomaxprocs", strings.NewReader(`{"value":`+fmt.Sprint(target)+`}`))
	response := httptest.NewRecorder()
	server.handleGOMAXPROCS(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var result gomaxprocsResponseObj
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !result.OK || result.Previous != original || result.Current != target || runtime.GOMAXPROCS(0) != target {
		t.Fatalf("unexpected response: %+v, runtime=%d", result, runtime.GOMAXPROCS(0))
	}
}

func TestHandleGOMAXPROCSRestoreIsIdempotent(t *testing.T) {
	server := &serverObj{}
	defer server.gomaxprocs.restore()
	value := 1
	setResult := server.gomaxprocs.set(value)
	if !setResult.OK || !setResult.Overridden {
		t.Fatalf("set response: %+v", setResult)
	}
	for range 2 {
		request := httptest.NewRequest(http.MethodPost, "/runtime/gomaxprocs", strings.NewReader(`{"restore":true}`))
		response := httptest.NewRecorder()
		server.handleGOMAXPROCS(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
		}
		var result gomaxprocsResponseObj
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if !result.OK || result.Overridden {
			t.Fatalf("restore response: %+v", result)
		}
	}
}

func TestHandleGOMAXPROCSRejectsAmbiguousRequest(t *testing.T) {
	value := 1
	for _, body := range []string{`{}`, fmt.Sprintf(`{"value":%d,"restore":true}`, value)} {
		server := &serverObj{}
		request := httptest.NewRequest(http.MethodPost, "/runtime/gomaxprocs", strings.NewReader(body))
		response := httptest.NewRecorder()
		server.handleGOMAXPROCS(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body %s: status = %d, response = %s", body, response.Code, response.Body.String())
		}
	}
}

func TestHandleGOMAXPROCSRejectsInvalidValue(t *testing.T) {
	original := runtime.GOMAXPROCS(0)
	server := &serverObj{}
	request := httptest.NewRequest(http.MethodPost, "/runtime/gomaxprocs", strings.NewReader(`{"value":0}`))
	response := httptest.NewRecorder()
	server.handleGOMAXPROCS(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if runtime.GOMAXPROCS(0) != original {
		t.Fatalf("GOMAXPROCS changed from %d to %d", original, runtime.GOMAXPROCS(0))
	}
}

func TestHandleGOMAXPROCSRequiresMutationToken(t *testing.T) {
	server := &serverObj{mutateToken: "secret"}
	request := httptest.NewRequest(http.MethodPost, "/runtime/gomaxprocs", strings.NewReader(`{"value":1}`))
	response := httptest.NewRecorder()
	server.handleGOMAXPROCS(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}
