package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/gagliardetto/solana-go/rpc"
)

func fileExistsAndIsNotEmpty(path string) bool {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	if info.Size() == 0 {
		return false
	}
	return true
}

func main() {
	var epoch uint64 = 0
	var rpcEndpoint string
	var providedPathToFaithfulSlotListFile string
	flag.Uint64Var(&epoch, "epoch", epoch, "The epoch to fetch blocks for.")
	flag.StringVar(&rpcEndpoint, "rpc", "", "The RPC endpoint to use.")
	flag.StringVar(&providedPathToFaithfulSlotListFile, "faithful", "", "The path to the faithful slot list file.")
	flag.Parse()
	if rpcEndpoint == "" {
		panic("rpc endpoint not specified")
	}
	epochStart, epochStop := CalcEpochLimits(epoch)
	fmt.Printf("Epoch %d: %d - %d\n", epoch, epochStart, epochStop)

	err := os.MkdirAll("lists/solana", 0o755)
	if err != nil {
		panic(fmt.Errorf("could not create dir lists/solana: %w", err))
	}

	pathFromFaithful := fmt.Sprintf("lists/faithful/%d.slots.txt", epoch)
	if providedPathToFaithfulSlotListFile != "" {
		pathFromFaithful = providedPathToFaithfulSlotListFile
	}
	fmt.Printf("Going to compare the list of slots for epoch %d from %s with the list from solana rpc.\n", epoch, mustAbs(pathFromFaithful))
	if !fileExistsAndIsNotEmpty(pathFromFaithful) {
		panic(fmt.Errorf("file %s does not exist or is empty", pathFromFaithful))
	}
	pathFromSolana := fmt.Sprintf("lists/solana/%d.slots.txt-solana", epoch)

	if fileExistsAndIsNotEmpty(pathFromFaithful) && fileExistsAndIsNotEmpty(pathFromSolana) {
		fmt.Printf("Comparing %s with %s\n", mustAbs(pathFromFaithful), mustAbs(pathFromSolana))
		// compare with list from file:
		fromFaithful, err := loadBlockListFromFile(pathFromFaithful)
		if err != nil {
			panic(err)
		}
		fromSolana, err := loadBlockListFromFile(pathFromSolana)
		if err != nil {
			panic(err)
		}

		fromFaithful = removeIf(fromFaithful, func(block uint64) bool {
			return CalcEpochForSlot(block) != epoch
		})
		fromSolana = removeIf(fromSolana, func(block uint64) bool {
			return CalcEpochForSlot(block) != epoch
		})

		compare(fromFaithful, fromSolana)
		return
	}
	client := rpc.New(rpcEndpoint)

	start, end := epochStart, epochStop
	if start > 0 {
		start--
	}

	blocks, err := getAllBlocksBetween(client, uint64(start), uint64(end))
	if err != nil {
		panic(err)
	}

	fromSolana := reduceBlocks(blocks)
	// save to file:
	{
		file, err := os.Create(pathFromSolana)
		if err != nil {
			panic(err)
		}
		defer file.Close()

		for _, block := range fromSolana {
			_, err := file.WriteString(strconv.FormatUint(block, 10) + "\n")
			if err != nil {
				panic(err)
			}
		}
		fmt.Printf("Saved slot list for epoch %d (from solana rpc) to %s\n", epoch, mustAbs(pathFromSolana))
	}

	fromFaithful, err := loadBlockListFromFile(pathFromFaithful)
	if err != nil {
		panic(err)
	}

	fromFaithful = removeIf(fromFaithful, func(block uint64) bool {
		return CalcEpochForSlot(block) != epoch
	})
	fromSolana = removeIf(fromSolana, func(block uint64) bool {
		return CalcEpochForSlot(block) != epoch
	})

	compare(fromFaithful, fromSolana)
}

func mustAbs(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		panic(err)
	}
	return abs
}

func removeIf(slice []uint64, remover func(uint64) bool) []uint64 {
	var out []uint64
	for _, item := range slice {
		if !remover(item) {
			out = append(out, item)
		}
	}
	return out
}

func compare(fromFaithful []uint64, fromSolana []uint64) {
	{
		hasDiff := false
		// blocks in faithful but not in solana:
		var notInSolana []uint64
		for _, block := range fromFaithful {
			if !contains(fromSolana, block) {
				notInSolana = append(notInSolana, block)
			}
		}
		if len(notInSolana) > 0 {
			fmt.Printf("🚫 blocks in %s but not in %s:\n", green("faithful"), red("solana"))
			for _, block := range notInSolana {
				fmt.Println(block)
			}
			hasDiff = true
		}

		// blocks in solana but not in faithful:
		var notInFaithful []uint64
		for _, block := range fromSolana {
			if !contains(fromFaithful, block) {
				notInFaithful = append(notInFaithful, block)
			}
		}
		if len(notInFaithful) > 0 {
			fmt.Printf("🚫 blocks in %s but not in %s:\n", green("solana"), red("faithful"))
			for _, block := range notInFaithful {
				fmt.Println(block)
			}
			hasDiff = true
		}

		if !hasDiff {
			fmt.Println("✅ No differences.")
		}
	}
}

func contains(slots []uint64, slot uint64) bool {
	i := SearchUint64(slots, slot)
	return i < len(slots) && slots[i] == slot
}

func SearchUint64(a []uint64, x uint64) int {
	return sort.Search(len(a), func(i int) bool { return a[i] >= x })
}

func loadBlockListFromFile(path string) ([]uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var out []uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		block, err := strconv.ParseUint(line, 10, 64)
		if err != nil {
			return nil, err
		}
		out = append(out, block)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return uniqueBlocks(out), nil
}

func reduceBlocks(blocks []rpc.BlocksResult) []uint64 {
	var out []uint64
	for _, blocksResult := range blocks {
		for _, block := range blocksResult {
			out = append(out, block)
		}
	}
	return uniqueBlocks(out)
}

func uniqueBlocks(blocks []uint64) []uint64 {
	// sort, then remove duplicates:
	sort.Slice(blocks, func(i, j int) bool {
		return blocks[i] < blocks[j]
	})
	var out []uint64
	for i := range blocks {
		if i == 0 || blocks[i] != blocks[i-1] {
			out = append(out, blocks[i])
		}
	}
	return out
}

func getAllBlocksBetween(client *rpc.Client, startSlot, endSlot uint64) ([]rpc.BlocksResult, error) {
	pageSize := uint64(1000)
	var out []rpc.BlocksResult
	index := 1
	// fetch in blocks of 1000:
	for startSlot <= endSlot {
		end := startSlot + pageSize - 1
		if end > endSlot {
			end = endSlot
		}
		fmt.Printf("%d · Fetching blocks between %d and %d", index, startSlot, end)
		startedAt := time.Now()
		blocks, err := client.GetBlocks(
			context.TODO(),
			startSlot,
			&end,
			rpc.CommitmentFinalized,
		)
		fmt.Printf(" · Fetched %d blocks in %s\n", len(blocks), time.Since(startedAt))
		if err != nil {
			return nil, err
		}
		out = append(out, blocks)
		startSlot = end + 1
		index++
	}
	return out, nil
}

func red(s string) string {
	return "\033[31m" + s + "\033[0m"
}

func green(s string) string {
	return "\033[32m" + s + "\033[0m"
}

const EpochLen = 432000

func CalcEpochLimits(epoch uint64) (uint64, uint64) {
	epochStart := epoch * EpochLen
	epochStop := epochStart + EpochLen - 1
	return epochStart, epochStop
}

// CalcEpochForSlot returns the epoch for the given slot.
func CalcEpochForSlot(slot uint64) uint64 {
	return slot / EpochLen
}
