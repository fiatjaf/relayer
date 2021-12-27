relayer: $(shell find .. -name "*.go")
	go build -ldflags="-s -w" -o ./relayer

deploy: relayer
	ssh root@turgot 'systemctl stop relayer'
	scp relayer turgot:relayer/relayer
	ssh root@turgot 'systemctl start relayer'
