package raft

//
// This is an outline of the API that raft must expose to
// the service (or tester). See comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   Create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   Start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   Each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester) in the same server.
//

import (
	"cs351/labrpc"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// As each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make(). Set
// CommandValid to true to indicate that the ApplyMsg contains a newly
// committed log entry.
type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int
}

// Log is a single log entry (Raft paper, Figure 2).
type Log struct {
	Operation interface{} // command for the state machine
	Term      int         // term when the entry was received by the leader
}

const candidate int = 0
const follower int = 1
const leader int = 2

// A Go object implementing a single Raft peer.
type Raft struct {
	mu    sync.Mutex          // Lock to protect shared access to this peer's state
	peers []*labrpc.ClientEnd // RPC end points of all peers
	me    int                 // This peer's index into peers[]
	dead  int32               // Set by Kill()

	// Persistent state on all servers (Figure 2)
	currentTerm int
	votedFor    int
	log         []Log

	// Volatile state on all servers
	commitIndex int
	lastApplied int

	// Volatile state on leaders
	nextIndex  []int
	matchIndex []int

	role          int           // follower, candidate, or leader
	lastHeartbeat time.Time     // time of the last heartbeat or granted vote
	applyCh       chan ApplyMsg // channel for sending committed entries to the service

}

// Return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {
	var term int
	var isleader bool
	rf.mu.Lock()
	term = rf.currentTerm
	isleader = false
	if rf.role == leader {
		isleader = true
	}
	rf.mu.Unlock()

	return term, isleader
}

// RequestVote RPC arguments (Figure 2).
// Field names must start with capital letters!
type RequestVoteArgs struct {
	Term         int
	CandidateId  int
	LastLogIndex int
	LastLogTerm  int
}

// RequestVote RPC reply (Figure 2).
// Field names must start with capital letters!
type RequestVoteReply struct {
	Term        int
	VoteGranted bool
}

// RequestVote RPC handler (receiver implementation, Figure 2).
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if args.Term < rf.currentTerm { // reject candidates from an older term
		reply.Term = rf.currentTerm
		reply.VoteGranted = false
		return
	}
	if args.Term > rf.currentTerm { // newer term: update our term, step down to follower, and clear our vote
		rf.currentTerm = args.Term
		rf.role = follower
		rf.votedFor = -1
	}
	if rf.votedFor == -1 || rf.votedFor == args.CandidateId { // only vote if we haven't voted this term, or already voted for this candidate

		// Grant the vote only if the candidate's log is at least as up-to-date as ours (Raft §5.4.1)
		if args.LastLogTerm > rf.log[len(rf.log)-1].Term { // candidate's last entry has a higher term
			reply.VoteGranted = true
			rf.votedFor = args.CandidateId
			rf.lastHeartbeat = time.Now()
		} else if args.LastLogTerm == rf.log[len(rf.log)-1].Term && args.LastLogIndex >= len(rf.log)-1 { // same last term, and candidate's log is at least as long
			reply.VoteGranted = true
			rf.votedFor = args.CandidateId
			rf.lastHeartbeat = time.Now()
		}
	}
	reply.Term = rf.currentTerm

}

// AppendEntries RPC arguments (Figure 2).
type AppendEntriesArgs struct {
	Term         int
	Leaderid     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []Log
	LeaderCommit int
}

// AppendEntries RPC reply (Figure 2).
type AppendEntriesReply struct {
	Term    int
	Success bool
}

// AppendEntries RPC handler, used for both heartbeats and log replication.
func (rf *Raft) AppendEntry(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if args.Term < rf.currentTerm { // reject leaders from an older term
		reply.Success = false
		reply.Term = rf.currentTerm
		return
	}

	if args.Term > rf.currentTerm { // newer term: update our term, become follower, and clear our vote
		rf.currentTerm = args.Term
		rf.role = follower
		rf.lastHeartbeat = time.Now()
		reply.Term = rf.currentTerm
		rf.votedFor = -1
	}
	if args.Term == rf.currentTerm { // current leader: stay follower and reset the election timer; our vote stays valid
		rf.currentTerm = args.Term
		rf.role = follower
		rf.lastHeartbeat = time.Now()
		reply.Term = rf.currentTerm

	}

	// Receiver implementation rules 2-5 (Figure 2)
	if args.PrevLogIndex >= len(rf.log) {
		reply.Success = false
		return
	}
	if rf.log[args.PrevLogIndex].Term != args.PrevLogTerm { // reply false if our log has no entry at prevLogIndex matching prevLogTerm
		reply.Success = false
		return
	}

	var invalid = false
	var index = -1
	for i := 0; i < len(args.Entries) && invalid == false; i++ { // find the first entry that conflicts with the leader's entries
		x := args.PrevLogIndex + 1 + i
		if x < len(rf.log) && rf.log[x].Term != args.Entries[i].Term {
			invalid = true
			index = x
		}
	}

	if invalid == true { // conflict found: delete it and everything after, then append the leader's entries
		rf.log = rf.log[:index]
		for i := 0; i < len(args.Entries); i++ {
			x := args.PrevLogIndex + 1 + i
			if x >= len(rf.log) {
				rf.log = append(rf.log, args.Entries[i])
			}
		}
	} else { // no conflict: append any entries we don't have yet
		for i := 0; i < len(args.Entries); i++ {
			x := args.PrevLogIndex + 1 + i
			if x >= len(rf.log) {
				rf.log = append(rf.log, args.Entries[i])
			}
		}
	}

	if args.LeaderCommit > rf.commitIndex {
		rf.commitIndex = min(args.LeaderCommit, len(rf.log)-1)
	}

	reply.Success = true
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

// Example code to send a RequestVote RPC to a server.
// Server is the index of the target server in rf.peers[].
// Expects RPC arguments in args. Fills in *reply with RPC reply,
// so caller should pass &reply.
//
// The types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// Look at the comments in ../labrpc/labrpc.go for more details.
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

func (rf *Raft) sendAppendEntry(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntry", args, reply)
	return ok
}

// The service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. If this
// server isn't the leader, returns false. Otherwise start the
// agreement and return immediately. There is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election. Even if the Raft instance has been killed,
// this function should return gracefully.
//
// The first return value is the index that the command will appear at
// if it's ever committed. The second return value is the current
// term. The third return value is true if this server believes it is
// the leader.
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	index := -1
	term := -1
	isLeader := false

	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.role != leader {
		isLeader = false
	} else {
		isLeader = true
		rf.log = append(rf.log, Log{Operation: command, Term: rf.currentTerm}) // leader appends the command to its log
		term = rf.currentTerm
		index = len(rf.log) - 1 // index of the new entry
	}

	return index, term, isLeader
}

// The tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. Your code can use killed() to
// check whether Kill() has been called. The use of atomic avoids the
// need for a lock.
//
// The issue is that long-running goroutines use memory and may chew
// up CPU time, perhaps causing later tests to fail and generating
// confusing debug output. Any goroutine with a long-running loop
// should call killed() to check whether it should stop.
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

// The ticker goroutine starts a new election if this peer hasn't
// received a heartbeat recently.
func (rf *Raft) ticker() {
	for rf.killed() == false {

		time.Sleep(10 * time.Millisecond)
		rf.mu.Lock()
		electionTimeout := time.Duration(300+rand.Intn(300)) * time.Millisecond // randomized election timeout (300-600 ms) to reduce split votes
		if rf.role != leader && time.Since(rf.lastHeartbeat) > electionTimeout {
			rf.mu.Unlock()
			rf.electionStart()
		} else {
			rf.mu.Unlock()
		}
	}
}

// last_Applied runs in the background and sends newly committed
// entries to the service, in log order.
func (rf *Raft) last_Applied() {
	for rf.killed() == false {
		time.Sleep(10 * time.Millisecond)
		rf.mu.Lock()
		if rf.commitIndex > rf.lastApplied {
			neededEntries := []ApplyMsg{}
			for rf.lastApplied < rf.commitIndex { // collect every entry up to commitIndex
				rf.lastApplied++
				neededEntries = append(neededEntries, ApplyMsg{CommandValid: true, Command: rf.log[rf.lastApplied].Operation, CommandIndex: rf.lastApplied}) // CommandValid marks this as a newly committed entry
			}
			rf.mu.Unlock()
			for i := 0; i < len(neededEntries); i++ {
				rf.applyCh <- neededEntries[i] // send outside the lock so a slow receiver can't block other operations
			}
		} else {
			rf.mu.Unlock()
		}

	}
}

// electionStart becomes a candidate and requests votes from all peers.
func (rf *Raft) electionStart() {
	rf.mu.Lock()
	// Rules for candidates (Figure 2)
	rf.role = candidate
	rf.currentTerm++
	rf.votedFor = rf.me
	rf.lastHeartbeat = time.Now() // reset the election timer so we don't start another election right away
	votesforme := 1

	// Snapshot the state to send in each RequestVote RPC
	term := rf.currentTerm
	lastLogIndex := len(rf.log) - 1
	lastLogTerm := rf.log[len(rf.log)-1].Term
	rf.mu.Unlock()
	for i := 0; i < len(rf.peers); i++ {
		if i != rf.me {
			go func(server_i int) {
				args := &RequestVoteArgs{Term: term, CandidateId: rf.me, LastLogIndex: lastLogIndex, LastLogTerm: lastLogTerm}
				reply := &RequestVoteReply{}
				success := rf.sendRequestVote(server_i, args, reply)

				// If the RPC went through, step down on a higher term; otherwise count the vote
				if success {
					rf.mu.Lock()
					defer rf.mu.Unlock()
					if reply.Term > rf.currentTerm {
						rf.role = follower
						rf.votedFor = -1            // our vote was for an outdated term, so clear it
						rf.currentTerm = reply.Term // adopt the newer term
						return
					}
					if reply.VoteGranted == true {
						votesforme++
						if votesforme > len(rf.peers)/2 && rf.role != leader { // won a majority: become leader
							rf.role = leader
							rf.nextIndex = make([]int, len(rf.peers))
							for i := 0; i < len(rf.peers); i++ {
								rf.nextIndex[i] = len(rf.log) - 1 + 1 // Figure 2: initialized to leader's last log index + 1
							}
							rf.matchIndex = make([]int, len(rf.peers))
							for i := 0; i < len(rf.peers); i++ {
								rf.matchIndex[i] = 0 // Figure 2: initialized to 0
							}

							go rf.heartbeatRPC()
						}
					}
				}
			}(i)
		}

	}
}

// heartbeatRPC runs while this peer is leader. It periodically sends
// AppendEntries RPCs to every peer, acting as heartbeats when there is
// nothing new to send, and advances commitIndex once a majority
// has replicated an entry.
func (rf *Raft) heartbeatRPC() {
	for rf.killed() == false {
		rf.mu.Lock()
		if rf.role == candidate || rf.role == follower { // no longer leader: stop sending heartbeats
			rf.mu.Unlock()
			return
		}

		term := rf.currentTerm
		leaderid := rf.me

		// Send AppendEntries to all peers concurrently
		for i := 0; i < len(rf.peers); i++ {
			if i != rf.me {
				PrevLogIndex := rf.nextIndex[i] - 1
				PrevLogTerm := rf.log[PrevLogIndex].Term
				entries := rf.log[rf.nextIndex[i]:] // every entry from nextIndex onward that this peer still needs
				LeaderCommit := rf.commitIndex
				go func(server_i int) {
					args := &AppendEntriesArgs{Term: term, Leaderid: leaderid, PrevLogIndex: PrevLogIndex, PrevLogTerm: PrevLogTerm, Entries: entries, LeaderCommit: LeaderCommit}
					reply := &AppendEntriesReply{}
					response := rf.sendAppendEntry(server_i, args, reply)
					if response == true {
						rf.mu.Lock()
						defer rf.mu.Unlock()
						if reply.Term > rf.currentTerm {
							rf.role = follower
							rf.votedFor = -1            // our vote was for an outdated term, so clear it
							rf.currentTerm = reply.Term // adopt the newer term
							return
						}
						if reply.Success == true { // follower accepted the entries
							rf.nextIndex[server_i] = PrevLogIndex + len(entries) + 1 // next entry to send is right after the ones just accepted
							rf.matchIndex[server_i] = rf.nextIndex[server_i] - 1     // highest entry known to be replicated on this peer
							// Commit entry N once a majority has it and it's from the current term (Figure 2)
							for N := rf.commitIndex + 1; N <= len(rf.log)-1; N++ {
								majority := 1 // the leader always has the entry
								for server := 0; server < len(rf.peers); server++ {
									if server != rf.me {
										if rf.matchIndex[server] >= N {
											majority = majority + 1
										}
									}
								}
								if majority >= len(rf.peers)/2+1 && rf.log[N].Term == rf.currentTerm {
									rf.commitIndex = N
								}
							}
						} else {
							rf.nextIndex[server_i] = rf.nextIndex[server_i] - 1 // log inconsistency: decrement nextIndex and retry (Figure 2)
						}
					}
				}(i)
			}
		}
		rf.mu.Unlock()
		time.Sleep(110 * time.Millisecond) // heartbeat interval; must be at least 100 ms

	}
}

// The service or tester wants to create a Raft server. The ports
// of all the Raft servers (including this one) are in peers[]. This
// server's port is peers[me]. All the servers' peers[] arrays
// have the same order. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
func Make(peers []*labrpc.ClientEnd, me int, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.me = me
	rf.applyCh = applyCh

	rf.currentTerm = 0
	rf.votedFor = -1                          // -1 means no vote yet
	rf.log = []Log{{Operation: nil, Term: 0}} // placeholder entry at index 0, since Raft log indexes start at 1
	rf.commitIndex = 0
	rf.lastApplied = 0
	rf.role = follower
	rf.lastHeartbeat = time.Now()

	// start the ticker goroutine to trigger elections
	go rf.ticker()
	// start the goroutine that applies committed entries
	go rf.last_Applied()

	return rf
}