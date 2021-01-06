package main

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"sort"

	"github.com/coinexchain/randsrc"

	"github.com/moeing-chain/MoeingDB/types"
	"github.com/moeing-chain/MoeingDB/modb"
)

type FuzzConfig struct {
	TotalAddressCount int
	TotalTopicCount   int
	TotalBlocks       int
	MaxLogInTx        int
	MaxTxInBlock      int
	QueryCount        int
}

var Config2 = FuzzConfig{
	TotalAddressCount: 15,
	TotalTopicCount:   30,
	TotalBlocks:       15,
	MaxLogInTx:        15,
	MaxTxInBlock:      15,
	QueryCount:        30,
}

var DefaultConfig = FuzzConfig{
	TotalAddressCount: 10,
	TotalTopicCount:   20,
	TotalBlocks:       5,
	MaxLogInTx:        5,
	MaxTxInBlock:      5,
	QueryCount:        20,
}

var AddressList [][20]byte
var TopicList [][32]byte

func initGlobal(rs randsrc.RandSrc, cfg FuzzConfig) {
	AddressList = make([][20]byte, cfg.TotalAddressCount)
	for i := range AddressList {
		copy(AddressList[i][:], rs.GetBytes(20))
	}
	TopicList = make([][32]byte, cfg.TotalTopicCount)
	for i := range TopicList {
		copy(TopicList[i][:], rs.GetBytes(32))
	}
}

func assert(b bool) {
	if !b {
		panic("fail")
	}
}

func main() {
	if len(os.Args) != 3 {
		fmt.Printf("Usage: %s <rand-source-file> <round-count>\n", os.Args[0])
		return
	}
	fname := os.Args[1]
	roundCount, err := strconv.Atoi(os.Args[2])
	if err != nil {
		panic(err)
	}

	cfg := Config2

	rs := randsrc.NewRandSrcFromFile(fname)
	initGlobal(rs, cfg)
	for i := 0; i < roundCount; i++ {
		if i % 5 == 0 {
			fmt.Printf("======== %d ========\n", i)
		}
		RunFuzz(rs, cfg)
	}
}

func GetLog(rs randsrc.RandSrc) (log types.Log) {
	n := int(rs.GetUint32()) % 5
	idx := int(rs.GetUint32()) % len(AddressList)
	copy(log.Address[:], AddressList[idx][:])
	if n == 0 {
		return
	}
	log.Topics = make([][32]byte, n)
	for i := range log.Topics {
		idx = int(rs.GetUint32()) % len(TopicList)
		copy(log.Topics[i][:], TopicList[idx][:])
	}
	return
}

func GetTx(rs randsrc.RandSrc, height int64, idx int, cfg FuzzConfig) (tx types.Tx) {
	copy(tx.HashId[:], rs.GetBytes(32))
	tx.Content = []byte(fmt.Sprintf("tx%d-%d", height, idx))
	n := int(rs.GetUint32()) % cfg.MaxLogInTx
	if n == 0 {
		return
	}
	tx.LogList = make([]types.Log, 0, n)
	for i := 0; i < n; i++ {
		tx.LogList = append(tx.LogList, GetLog(rs))
	}
	return
}

func GetBlock(rs randsrc.RandSrc, h int64, cfg FuzzConfig) (blk *types.Block) {
	blk = &types.Block{Height: h}
	copy(blk.BlockHash[:], rs.GetBytes(32))
	blk.BlockInfo = []byte(fmt.Sprintf("block%d", h))
	n := int(rs.GetUint32()) % cfg.MaxTxInBlock
	if n == 0 {
		return
	}
	blk.TxList = make([]types.Tx, 0, n)
	for i := 0; i < n; i++ {
		blk.TxList = append(blk.TxList, GetTx(rs, h, i, cfg))
	}
	return
}

func Query(db types.DB, useAddr bool, log types.Log, startHeight, endHeight uint32) map[string]struct{} {
	txSet := make(map[string]struct{})
	addr := &log.Address
	if !useAddr {
		addr = nil
	}
	db.QueryLogs(addr, log.Topics, startHeight, endHeight, func(info []byte) bool {
		txSet[string(info)] = struct{}{}
		return true
	})
	return txSet
}

func mapToSortedStrings(m map[string]struct{}) []string {
	res := make([]string, 0, len(m))
	for k := range m {
		res = append(res, k)
	}
	sort.Strings(res)
	return res
}

func RunFuzz(rs randsrc.RandSrc, cfg FuzzConfig) {
	ref := &modb.MockMoDB{}
	os.RemoveAll("./test")
	os.Mkdir("./test", 0700)
	os.Mkdir("./test/data", 0700)
	imp := modb.CreateEmptyMoDB("./test", [8]byte{1,2,3,4,5,6,7,8})
	defer imp.Close()
	blkList := make([]*types.Block, 0, cfg.TotalBlocks)
	for h := int64(0); h < int64(cfg.TotalBlocks); h++ {
		blk := GetBlock(rs, h, cfg)
		blkList = append(blkList, blk)
		ref.AddBlock(blk, -1)
		imp.AddBlock(blk, -1)
	}
	imp.AddBlock(nil, -1)
	for h, blk := range blkList {
		if !bytes.Equal(imp.GetBlockByHeight(int64(h)), blk.BlockInfo) {
			fmt.Printf("Why %s %s\n", string(imp.GetBlockByHeight(int64(h))), string(blk.BlockInfo))
		}
		assert(bytes.Equal(imp.GetBlockByHeight(int64(h)), blk.BlockInfo))
		foundIt := false
		imp.GetBlockByHash(blk.BlockHash, func(info []byte) bool {
			if bytes.Equal(blk.BlockInfo, info) {
				foundIt = true
				return true
			}
			return false
		})
		assert(foundIt)
		for idx, tx := range blk.TxList {
			assert(bytes.Equal(imp.GetTxByHeightAndIndex(int64(h), idx), tx.Content))
			imp.GetTxByHash(tx.HashId, func(content []byte) bool {
				if bytes.Equal(tx.Content, content) {
					foundIt = true
					return true
				}
				return false
			})
			assert(foundIt)
		}
	}
	for i := 0; i < cfg.QueryCount; i++ {
		//fmt.Printf(" ------- Query %d --------\n", i)
		startHeight := rs.GetUint32() % uint32(cfg.TotalBlocks)
		endHeight := rs.GetUint32() % uint32(cfg.TotalBlocks)
		if startHeight > endHeight {
			startHeight, endHeight = endHeight, startHeight
		}
		log := GetLog(rs)
		useAddr := (int(rs.GetUint32()) % 2) == 0
		if len(log.Topics) == 0 && !useAddr {
			continue
		}
		refSet := Query(ref, useAddr, log, startHeight, endHeight)
		impSet := Query(imp, useAddr, log, startHeight, endHeight)
		ok := true
		for tx := range refSet {
			if _, ok = impSet[tx]; !ok {
				break
			}
		}
		if !ok {
			fmt.Printf("refSet: %s\n", mapToSortedStrings(refSet))
			fmt.Printf("impSet: %s\n", mapToSortedStrings(impSet))
			fmt.Printf("startHeight %d endHeight %d; useAddr %v; Addr and Topics:\n%#v\n",
				startHeight, endHeight, useAddr, log)
			PrintBlocks(blkList)

			//impSet = Query(imp, false, log, startHeight, endHeight)
			//fmt.Printf("impSet Only Log: %s\n", mapToSortedStrings(impSet))
			//log.Topics = nil
			fmt.Printf(":::::::::::::::::::::::::::::::::::::\n")
			impSet = Query(imp, true, log, startHeight, endHeight+1)
			fmt.Printf("impSet Only Addr: %s\n", mapToSortedStrings(impSet))
		}
		assert(ok)
	}
}

func PrintBlocks(blkList []*types.Block) {
	for i, blk := range blkList {
		fmt.Printf("The Block %d:\n%#v\n", i, blk)
	}
}
