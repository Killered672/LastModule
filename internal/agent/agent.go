package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"calc_service/internal/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	ErrDivisionByZero  = errors.New("division by zero")
	ErrInvalidOperator = errors.New("invalid operator")
)

type Agent struct {
	ComputingPower  int
	OrchestratorURL string
	Conn            *grpc.ClientConn
	Client          FinalProject.CalculatorClient
}

func NewAgent() *Agent {
	cp, err := strconv.Atoi(os.Getenv("COMPUTING_POWER"))
	if err != nil || cp < 1 {
		cp = 1
	}

	orchestratorURL := os.Getenv("ORCHESTRATOR_URL")
	if orchestratorURL == "" {
		orchestratorURL = "localhost:50051"
	}

	conn, err := grpc.Dial(
		orchestratorURL,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithTimeout(5*time.Second),
	)
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}

	client := FinalProject.NewCalculatorClient(conn)

	return &Agent{
		ComputingPower:  cp,
		OrchestratorURL: orchestratorURL,
		Conn:            conn,
		Client:          client,
	}
}

func (a *Agent) Start() {
	defer a.Conn.Close()

	for i := 0; i < a.ComputingPower; i++ {
		log.Printf("Starting worker %d", i)
		go a.Worker(i)
	}

	select {}
}

func (a *Agent) Worker(id int) {
	for {
		task, err := a.Client.GetTask(context.Background(), &FinalProject.TaskRequest{
			ComputingPower: int32(a.ComputingPower),
		})
		if err != nil {
			log.Printf("Worker %d: error getting task: %v", id, err)
			time.Sleep(2 * time.Second)
			continue
		}

		if task.Id == "" {
			time.Sleep(1 * time.Second)
			continue
		}

		log.Printf("Worker %d: received task %s: %f %s %f, simulating %d ms",
			id, task.Id, task.Arg1, task.Operation, task.Arg2, task.OperationTime)

		time.Sleep(time.Duration(task.OperationTime) * time.Millisecond)

		result, err := Calculations(task.Operation, task.Arg1, task.Arg2)
		if err != nil {
			log.Printf("Worker %d: error computing task %s: %v", id, task.Id, err)
			continue
		}

		_, err = a.Client.SubmitResult(context.Background(), &FinalProject.ResultRequest{
			Id:     task.Id,
			Result: result,
		})
		if err != nil {
			log.Printf("Worker %d: error submitting result for task %s: %v", id, task.Id, err)
			continue
		}

		log.Printf("Worker %d: successfully completed task %s with result %f", id, task.Id, result)
	}
}

func CalculateExpression(expression string) (float64, error) {
	return 0, fmt.Errorf("not implemented")
}

func Calculations(operation string, a, b float64) (float64, error) {
	switch operation {
	case "+":
		return a + b, nil
	case "-":
		return a - b, nil
	case "*":
		return a * b, nil
	case "/":
		if b == 0 {
			return 0, ErrDivisionByZero
		}
		return a / b, nil
	default:
		return 0, fmt.Errorf("invalid operator: %s", operation)
	}
}
