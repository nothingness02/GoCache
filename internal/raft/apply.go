package raft

import (
	"encoding/json"
	"log"
	"time"
)

// applyLoop 定期将已提交的日志应用到存储引擎
func (n *RaftNode) applyLoop() {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-n.stopCh:
			return
		case <-ticker.C:
			n.mu.Lock()
			for n.lastApplied < n.commitIndex {
				n.lastApplied++
				if n.lastApplied > uint64(len(n.log)) {
					log.Printf("[Raft] applyLoop: lastApplied %d > log length %d", n.lastApplied, len(n.log))
					n.lastApplied = uint64(len(n.log))
					break
				}
				entry := n.log[n.lastApplied-1]
				// no-op entry (empty command) — skip
				if len(entry.Command) == 0 {
					continue
				}
				var cmd Command
				if err := json.Unmarshal(entry.Command, &cmd); err != nil {
					log.Printf("[Raft] failed to unmarshal command at index %d: %v", entry.Index, err)
					continue
				}
				switch cmd.Op {
				case "set":
					n.storage.ApplySet(cmd.Key, cmd.Value, cmd.TTL)
				case "del":
					n.storage.ApplyDelete(cmd.Key)
				default:
					log.Printf("[Raft] unknown command op: %s", cmd.Op)
				}
			}
			shouldSnapshot := n.lastApplied > n.lastSnapshotIndex && n.lastApplied-n.lastSnapshotIndex > snapshotThreshold
			n.mu.Unlock()

			if shouldSnapshot {
				if err := n.takeSnapshot(); err != nil {
					log.Printf("[Raft] snapshot failed: %v", err)
				}
			}
		}
	}
}
