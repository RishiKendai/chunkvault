BIN    := chunkvault
REPO   ?= ./cv-repo
INPUT  ?= data/100mb.txt
OUTPUT ?= data/restored-100mb.txt
LIST   ?= $(REPO)/lists/$(notdir $(INPUT)).list

.PHONY: bin backup restore verify roundtrip clean bench

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

bench: bin
	./$(BIN) bench

# usage
# make roundtrip                                   # uses the defaults above
# make roundtrip INPUT=data/photo.jpg OUTPUT=/tmp/photo.jpg
# make restore LIST=cv-repo/lists/data.txt.20261017T080000Z.list OUTPUT=/tmp/old.txt
# make clean                                       # wipes the repo and binary, so use it carefully