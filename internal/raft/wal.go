package raft

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WAL 定义 Raft 持久化状态接口
type WAL interface {
	SaveState(term uint64, votedFor string, log []LogEntry) error
	LoadState() (uint64, string, []LogEntry, error)
	Close() error
}

// FileWAL 基于文件的 WAL 实现，将状态与日志拆分为两个文件原子写入
type FileWAL struct {
	statePath string
	logPath   string
}

// NewFileWAL 创建基于文件的 WAL
func NewFileWAL(dataDir string) (*FileWAL, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create WAL dir: %w", err)
	}
	return &FileWAL{
		statePath: filepath.Join(dataDir, "raft.state"),
		logPath:   filepath.Join(dataDir, "raft.log"),
	}, nil
}

// SaveState 原子写入当前 term、votedFor 和完整日志
func (w *FileWAL) SaveState(term uint64, votedFor string, log []LogEntry) error {
	state := struct {
		Term     uint64 `json:"term"`
		VotedFor string `json:"voted_for"`
	}{Term: term, VotedFor: votedFor}
	stateData, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := atomicWriteFile(w.statePath, stateData); err != nil {
		return fmt.Errorf("write state failed: %w", err)
	}

	logData, err := json.Marshal(log)
	if err != nil {
		return err
	}
	if err := atomicWriteFile(w.logPath, logData); err != nil {
		return fmt.Errorf("write log failed: %w", err)
	}
	return nil
}

// LoadState 加载持久化状态
func (w *FileWAL) LoadState() (uint64, string, []LogEntry, error) {
	var term uint64
	var votedFor string
	var log []LogEntry

	stateData, err := os.ReadFile(w.statePath)
	if err != nil && !os.IsNotExist(err) {
		return 0, "", nil, fmt.Errorf("read state failed: %w", err)
	}
	if err == nil {
		var state struct {
			Term     uint64 `json:"term"`
			VotedFor string `json:"voted_for"`
		}
		if err := json.Unmarshal(stateData, &state); err != nil {
			return 0, "", nil, fmt.Errorf("unmarshal state failed: %w", err)
		}
		term = state.Term
		votedFor = state.VotedFor
	}

	logData, err := os.ReadFile(w.logPath)
	if err != nil && !os.IsNotExist(err) {
		return 0, "", nil, fmt.Errorf("read log failed: %w", err)
	}
	if err == nil {
		if err := json.Unmarshal(logData, &log); err != nil {
			return 0, "", nil, fmt.Errorf("unmarshal log failed: %w", err)
		}
	}

	return term, votedFor, log, nil
}

// Close 关闭 WAL（无资源需要释放）
func (w *FileWAL) Close() error {
	return nil
}

func atomicWriteFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
