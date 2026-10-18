BIN    := chunkvault
REPO   ?= ./cv-repo
INPUT  ?= data/100mb.txt
OUTPUT ?= data/restored-100mb.txt
# LIST   ?= $(REPO)/lists/$(notdir $(INPUT)).list
SNAP ?= latest

.PHONY: bin backup restore verify roundtrip clean bench crash

bin:
	go build -o $(BIN) ./cmd/chunkvault

backup: bin
	./$(BIN) backup --repo $(REPO) $(INPUT)

snapshots: bin
	./$(BIN) snapshots --repo $(REPO)

restore: bin
	./$(BIN) restore --repo $(REPO) $(SNAP) $(OUTPUT)

check:
	diff -r $(INPUT) $(OUTPUT) && echo "IDENTICAL"

verify: bin
	./$(BIN) verify --repo $(REPO) --read-data

crash: bin
	bash test/chaos/crash_loop.sh

roundtrip: backup restore check

clean:
	rm -rf $(REPO) $(BIN)

bench: bin
	./$(BIN) bench

# usage
# make roundtrip                                   # uses the defaults above
# make roundtrip INPUT=data/photo.jpg OUTPUT=/tmp/photo.jpg
# make restore LIST=cv-repo/lists/data.txt.20261017T080000Z.list OUTPUT=/tmp/old.txt
# make clean                                       # wipes the repo and binary, so use it carefully