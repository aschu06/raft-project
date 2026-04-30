# Raft Consensus Project

## Description
This project implements the Raft consensus algorithm to maintain a consistent replicated state across multiple distributed nodes. Each node can act as a follower, candidate, or leader, and the system ensures agreement on a sequence of log entries even in the presence of failures.

## Features

### Leader Election
- Nodes start as followers and transition to candidates if they do not receive heartbeats
- Candidates request votes from other nodes using RequestVote RPCs
- Election timeouts are randomized to reduce split votes
- A node becomes leader after receiving a majority of votes

### Log Replication
- The leader accepts client commands and appends them to its log
- Entries are replicated to followers using AppendEntries RPCs
- Followers validate log consistency using previous log index and term
- Committed entries are applied in order to the state machine

### Fault Tolerance
- Handles leader failure by triggering a new election
- Maintains consistency during network delays or dropped messages
- Ensures safety: no two leaders can exist in the same term

## Tech Used
- Go
- Goroutines for concurrent node behavior
- Mutexes for thread-safe state access
- Channels (if you used them—remove if not)

## System Design
- Each server maintains:
  - Current term
  - VotedFor
  - Log entries
  - Commit index
- RPC communication is used for:
  - RequestVote
  - AppendEntries
- State transitions:
  - Follower → Candidate → Leader

## How to Run

1. Clone the repository:
```bash
git clone https://github.com/yourusername/raft-project.git
cd raft-project
## How to Run

### Prerequisites
- Go installed (version 1.18+ recommended)

### Steps

# Run 3 nodes (open 3 separate terminals)

# Terminal 1
go run main.go --id=1 --port=8001

# Terminal 2
go run main.go --id=2 --port=8002

# Terminal 3
go run main.go --id=3 --port=8003
