package raft

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const snapshotThreshold = 10000

// SnapshotMeta 快照元数据
type SnapshotMeta struct {
	LastIncludedIndex uint64
	LastIncludedTerm  uint64
	Data              []byte
}

// takeSnapshot 触发日志压缩和快照（调用方需确保 n.mu 已解锁）
func (n *RaftNode) takeSnapshot() error {
	n.mu.Lock()
	if n.lastApplied <= n.lastSnapshotIndex {
		n.mu.Unlock()
		return nil
	}
	lastIncludedIndex := n.lastApplied
	lastIncludedTerm := uint64(0)
	if lastIncludedIndex > 0 && lastIncludedIndex <= uint64(len(n.log)) {
		lastIncludedTerm = n.log[lastIncludedIndex-1].Term
	}
	n.mu.Unlock()

	data, err := n.storage.Snapshot()
	if err != nil {
		return fmt.Errorf("storage snapshot failed: %w", err)
	}

	meta := &SnapshotMeta{
		LastIncludedIndex: lastIncludedIndex,
		LastIncludedTerm:  lastIncludedTerm,
		Data:              data,
	}
	path := n.snapshotPath(lastIncludedIndex)
	if err := saveSnapshot(path, meta); err != nil {
		return fmt.Errorf("save snapshot failed: %w", err)
	}

	n.mu.Lock()
	n.lastSnapshotIndex = lastIncludedIndex
	n.saveState()
	n.mu.Unlock()

	n.cleanOldSnapshots(path)
	return nil
}

func (n *RaftNode) snapshotPath(index uint64) string {
	return filepath.Join(n.dataDir, fmt.Sprintf("snapshot-%d.bin", index))
}

func saveSnapshot(path string, meta *SnapshotMeta) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer f.Close()

	header := make([]byte, 20)
	binary.LittleEndian.PutUint64(header[0:8], meta.LastIncludedIndex)
	binary.LittleEndian.PutUint64(header[8:16], meta.LastIncludedTerm)
	binary.LittleEndian.PutUint32(header[16:20], uint32(len(meta.Data)))
	if _, err := f.Write(header); err != nil {
		return err
	}
	if _, err := f.Write(meta.Data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func loadSnapshot(path string) (*SnapshotMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 20 {
		return nil, fmt.Errorf("snapshot file too short")
	}
	dataLen := binary.LittleEndian.Uint32(data[16:20])
	if uint32(len(data)) != 20+dataLen {
		return nil, fmt.Errorf("snapshot data length mismatch")
	}
	return &SnapshotMeta{
		LastIncludedIndex: binary.LittleEndian.Uint64(data[0:8]),
		LastIncludedTerm:  binary.LittleEndian.Uint64(data[8:16]),
		Data:              data[20:],
	}, nil
}

func (n *RaftNode) latestSnapshot() (string, *SnapshotMeta, error) {
	entries, err := os.ReadDir(n.dataDir)
	if err != nil {
		return "", nil, err
	}
	var latestIndex uint64
	var latestPath string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "snapshot-") {
			continue
		}
		idxStr := strings.TrimPrefix(strings.TrimSuffix(entry.Name(), ".bin"), "snapshot-")
		idx, err := strconv.ParseUint(idxStr, 10, 64)
		if err != nil {
			continue
		}
		if idx > latestIndex {
			latestIndex = idx
			latestPath = filepath.Join(n.dataDir, entry.Name())
		}
	}
	if latestPath == "" {
		return "", nil, nil
	}
	meta, err := loadSnapshot(latestPath)
	if err != nil {
		return "", nil, err
	}
	return latestPath, meta, nil
}

func (n *RaftNode) cleanOldSnapshots(keepPath string) {
	entries, err := os.ReadDir(n.dataDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		path := filepath.Join(n.dataDir, entry.Name())
		if path != keepPath && strings.HasPrefix(entry.Name(), "snapshot-") {
			os.Remove(path)
		}
	}
}
