package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"calc_service/internal/agent"
	"calc_service/internal/orchestrator"
	"calc_service/internal/storage"
)

func startTestServices(t *testing.T) (func(), string) {
	dbPath := "test_integration.db"
	os.Remove(dbPath)

	storage, err := storage.NewStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	storage.Init()

	orch := orchestrator.NewOrchestrator()
	orch.Storage = storage
	go func() {
		if err := orch.RunServer(); err != nil {
			t.Logf("Orchestrator failed: %v", err)
		}
	}()

	ag := agent.NewAgent()
	ag.OrchestratorURL = "localhost:50051"
	go ag.Start()

	time.Sleep(2 * time.Second)

	cleanup := func() {
		storage.GetDB().Close()
		os.Remove(dbPath)
	}

	resp, err := http.Post("http://localhost:8080/api/v1/register", "application/json",
		bytes.NewReader([]byte(`{"login":"testuser","password":"testpass"}`)))
	if err != nil {
		t.Fatalf("Failed to register user: %v", err)
	}
	defer resp.Body.Close()

	var authResp struct {
		Token string `json:"token"`
	}
	json.NewDecoder(resp.Body).Decode(&authResp)

	return cleanup, authResp.Token
}

func TestFullWorkflow(t *testing.T) {
	cleanup, token := startTestServices(t)
	defer cleanup()

	reqBody := []byte(`{"expression":"2+2*2"}`)
	req, _ := http.NewRequest("POST", "http://localhost:8080/api/v1/calculate", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Calculate request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("Expected status 201, got %d", resp.StatusCode)
	}

	var calcResp struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&calcResp)
	if calcResp.ID == "" {
		t.Error("Empty expression ID in response")
	}

	var exprStatus string
	var result *float64
	for i := 0; i < 10; i++ {
		time.Sleep(1 * time.Second)

		req, _ = http.NewRequest("GET", "http://localhost:8080/api/v1/expressions/"+calcResp.ID, nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Get expression failed: %v", err)
		}

		var exprResp struct {
			Expression struct {
				Status string   `json:"status"`
				Result *float64 `json:"result"`
			} `json:"expression"`
		}
		json.NewDecoder(resp.Body).Decode(&exprResp)
		resp.Body.Close()

		exprStatus = exprResp.Expression.Status
		result = exprResp.Expression.Result

		if exprStatus == "completed" {
			break
		}
	}

	if exprStatus != "completed" {
		t.Errorf("Expression not completed, status: %s", exprStatus)
	}

	if result == nil || *result != 6 {
		t.Errorf("Incorrect result, got: %v, expected: 6", result)
	}

	req, _ = http.NewRequest("GET", "http://localhost:8080/api/v1/expressions", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Get expressions failed: %v", err)
	}
	defer resp.Body.Close()

	var exprsResp struct {
		Expressions []struct {
			ID     string   `json:"id"`
			Result *float64 `json:"result"`
			Status string   `json:"status"`
		} `json:"expressions"`
	}
	json.NewDecoder(resp.Body).Decode(&exprsResp)

	if len(exprsResp.Expressions) != 1 ||
		exprsResp.Expressions[0].ID != calcResp.ID ||
		*exprsResp.Expressions[0].Result != 6 {
		t.Errorf("Unexpected expressions list: %+v", exprsResp)
	}
}

func TestDivisionByZero(t *testing.T) {
	cleanup, token := startTestServices(t)
	defer cleanup()

	reqBody := []byte(`{"expression":"10/0"}`)
	req, _ := http.NewRequest("POST", "http://localhost:8080/api/v1/calculate", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Calculate request failed: %v", err)
	}
	defer resp.Body.Close()

	var calcResp struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&calcResp)

	time.Sleep(3 * time.Second)

	req, _ = http.NewRequest("GET", "http://localhost:8080/api/v1/expressions/"+calcResp.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Get expression failed: %v", err)
	}
	defer resp.Body.Close()

	var exprResp struct {
		Expression struct {
			Status string   `json:"status"`
			Result *float64 `json:"result"`
		} `json:"expression"`
	}
	json.NewDecoder(resp.Body).Decode(&exprResp)

	if exprResp.Expression.Status != "error" {
		t.Errorf("Expected status 'error', got '%s'", exprResp.Expression.Status)
	}
}
