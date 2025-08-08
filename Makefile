.PHONY: run build test clean

# 默认目标
all: build

# 编译
build:
	go build -o bin/server main.go

# 运行开发服务器
run:
	go run main.go

# 运行测试
test:
	go test -v ./...

# 清理编译文件
clean:
	rm -rf bin/

# 检查代码格式
fmt:
	go fmt ./...

# 静态检查
vet:
	go vet ./...

# 生成依赖
deps:
	go mod download
	go mod tidy
