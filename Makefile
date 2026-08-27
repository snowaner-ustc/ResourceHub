.PHONY: server agent web tidy test

tidy:
	cd server && go mod tidy
	cd agent && go mod tidy

server:
	cd server && go run ./cmd/server

agent:
	cd agent && go run ./cmd/agent --server http://127.0.0.1:8080

web:
	cd web && npm install && npm run dev

test:
	cd server && go test ./...
	cd agent && go test ./...
