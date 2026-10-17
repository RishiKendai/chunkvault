BIN    := chunkvault
REPO   ?= ./cv-repo
INPUT  ?= data/data.txt
OUTPUT ?= /tmp/restored.txt
LIST   ?= $(REPO)/lists/$(notdir $(INPUT)).latest.list

.PHONY: bin backup restore verify roundtrip clean

bin:
	go build -o $(BIN) ./cmd/chunkvault

backup: bin
	./$(BIN) backup --repo $(REPO) $(INPUT)

restore: bin
	./$(BIN) restore --repo $(REPO) $(LIST) $(OUTPUT)

verify:
	cmp $(INPUT) $(OUTPUT) && echo "IDENTICAL"

roundtrip: backup restore verify

clean:
	rm -rf $(REPO) $(BIN)