# 🔧 MULTI-CHAIN INDEXER – COMPLETE ARCHITECTURE (DOCUMENT ONLY)

## 0. Core Requirements

* Minimize RPC calls (free tier constraints)
* Handle missing blocks detection & backfill
* Detect and handle blockchain reorganizations
* Emit standardized transaction events
* Support multi-chain without touching core
* Simple, maintainable design

---

## 1. System Architecture Overview

```
┌─────────────────────────────────────────┐
│         Control Plane                   │
│  - Health Monitor                       │
│  - RPC Budget Tracker                   │
│  - Missing Block Detector               │
│  - Reorg Detector                       │
└──────────────┬──────────────────────────┘
               │
┌──────────────▼──────────────────────────┐
│         Indexing Pipeline               │
│                                         │
│  Scheduler → Fetch → Filter → Emit      │
└──────────────┬──────────────────────────┘
               │
┌──────────────▼──────────────────────────┐
│         Recovery Plane                  │
│  - Missing Block Queue                  │
│  - Reorg Rollback Handler               │
│  - Failed Block Retry Queue             │
└─────────────────────────────────────────┘
```

---

## 2. Missing Block Detection & Handling

### 2.1 Detection Mechanism

**Problem:**
- Scheduler may skip blocks due to errors
- Provider may return gaps
- Network issues may cause skips
- System restarts may leave gaps

**Detection Strategy:**

**Method 1: Sequential Gap Check**
```
Current cursor: 1000
Fetch next block: 1005

Gap detected: blocks 1001, 1002, 1003, 1004 missing
```

**Method 2: Periodic Validation**
```
Every 1000 blocks:
- Query database for block sequence
- Find gaps in stored blocks
- Add to missing block queue
```

**Method 3: External Trigger**
```
Admin API call:
- Check blocks from X to Y
- Report gaps
- Option to auto-backfill
```

### 2.2 Missing Block Queue

**Queue Properties:**
- Persistent storage (database table)
- Priority ordering (older gaps = higher priority)
- Status tracking (pending/processing/completed/failed)
- Retry count
- Last attempt timestamp

**Queue Operations:**
- Add missing range
- Pop next range to process
- Mark as completed
- Mark as failed with retry

**Processing Strategy:**
```
Normal mode: Process current blocks
Background mode: Process missing blocks

Priority:
1. Recent missing blocks (< 100 blocks behind)
2. Old missing blocks (> 100 blocks behind)
3. Failed blocks with retry

Rate limiting:
- Process 10 missing blocks per minute
- Don't impact current block indexing
- Use separate RPC quota allocation
```

### 2.3 Backfill Modes

**Mode 1: Auto Backfill**
- Automatically process missing blocks
- Runs in background
- Low priority vs current blocks

**Mode 2: Manual Backfill**
- Triggered by admin
- Can specify block range
- Can use higher RPC quota
- Can run in parallel with normal indexing

**Mode 3: Scheduled Backfill**
- Runs during low traffic hours
- Catches up historical gaps
- Can use paid RPC if needed

---

## 3. Blockchain Reorganization Handling

### 3.1 What is Reorg

**Scenario:**
```
Original chain:
Block 999 → Block 1000A → Block 1001A → Block 1002A

Network reorganizes:
Block 999 → Block 1000B → Block 1001B → Block 1002B

Your indexer indexed blocks 1000A, 1001A, 1002A
These blocks are now INVALID (orphaned)
```

**Impact:**
- Indexed transactions may not exist on main chain
- Events emitted may be incorrect
- Need to rollback and re-index

### 3.2 Reorg Detection Strategy

**Method 1: Block Hash Verification**
```
Stored in DB:
- Block 1000: hash = 0xabc123...

Fetch from RPC:
- Block 1000: hash = 0xdef456...

Mismatch detected → Reorg happened
```

**Method 2: Parent Hash Check**
```
Fetch block 1001:
- parent_hash = 0xabc123

Check DB block 1000:
- hash = 0xdef456

Mismatch → Reorg detected
```

**Method 3: Finality-Based (Chain Specific)**
```
Ethereum: 12 blocks for practical finality
Bitcoin: 6 blocks for finality
Sui: Instant finality (no reorg possible)

Strategy:
- Only emit events for finalized blocks
- Keep unfinalized blocks in temporary storage
- Verify before finalizing
```

### 3.3 Reorg Detection Workflow

**Continuous Monitoring:**
```
Every block fetch:
1. Get new block N
2. Verify block N-12 hash matches DB (finality depth)
3. If mismatch: Reorg detected
4. If match: Continue normal operation
```

**Detection Triggers:**
- Regular block processing
- Periodic validation (every 100 blocks)
- Failed block hash verification
- External alert (provider notification)

### 3.4 Reorg Rollback Process

**Step 1: Identify Reorg Depth**
```
Start from current block, go backwards:
- Block 1002: hash mismatch → reorg
- Block 1001: hash mismatch → reorg
- Block 1000: hash mismatch → reorg
- Block 999: hash MATCH → safe point

Reorg depth: 3 blocks
Safe rollback point: 999
```

**Step 2: Mark Affected Data**
```
Mark in database:
- Blocks 1000-1002: status = "orphaned"
- Transactions in those blocks: status = "invalid"
- Events emitted: status = "reverted"
```

**Step 3: Emit Revert Events**
```
For each transaction that was emitted:
- Send revert event to downstream services
- Include original event ID for tracking
- Mark as reverted in event log
```

**Step 4: Rollback Cursor**
```
Update cursor:
- From: 1002
- To: 999 (safe point)
```

**Step 5: Re-index Correct Chain**
```
Start indexing from block 1000 (new chain):
- Fetch block 1000B
- Process transactions
- Emit new events
```

### 3.5 Reorg Event Notification

**Revert Event Schema:**
```
Event Type: "transaction_reverted"

Fields:
- original_event_id
- original_tx_hash
- reverted_block_number
- reorg_detected_at
- reason: "blockchain_reorganization"
```

**New Transaction Event:**
```
Event Type: "transaction_confirmed"

Fields:
- Same schema as regular transaction
- Additional: replaced_tx_hash (if replacing orphaned TX)
```

### 3.6 Reorg Prevention Strategies

**Strategy 1: Finality Wait**
```
Don't emit events immediately:
- Wait for N confirmations
- Ethereum: wait 12 blocks
- Bitcoin: wait 6 blocks
- Only emit after finality

Tradeoff:
- Pros: No revert events needed
- Cons: 2-3 minute delay in events
```

**Strategy 2: Probabilistic Emit**
```
Emit events at different confidence levels:
- 1 confirmation: status = "pending" (low confidence)
- 6 confirmations: status = "confirming" (medium)
- 12+ confirmations: status = "confirmed" (high)

Let downstream services decide risk tolerance
```

**Strategy 3: Safe Block Only**
```
Only index "safe" or "finalized" blocks from RPC:
- eth_getBlockByNumber with "safe" tag
- Guaranteed no reorg
- May lag behind by 12 blocks

Best for:
- Critical financial applications
- When correctness > speed
```

---

## 4. Error Recovery & Retry System

### 4.1 Failure Categories

**Category 1: Transient RPC Failures**
- Network timeout
- Rate limit hit
- Provider temporary down
- Solution: Retry with backoff

**Category 2: Data Parsing Failures**
- Unexpected block format
- Missing fields
- Invalid transaction data
- Solution: Log, alert, manual review

**Category 3: Downstream Failures**
- Event emitter unavailable
- Database connection lost
- Solution: Queue events, retry later

**Category 4: Permanent Failures**
- Block doesn't exist
- Invalid block number
- Corrupted data
- Solution: Mark as failed, manual intervention

### 4.2 Retry Strategy

**Exponential Backoff:**
```
Attempt 1: Immediate
Attempt 2: 2 seconds
Attempt 3: 4 seconds
Attempt 4: 8 seconds
Attempt 5: 16 seconds
Max: 60 seconds

Max attempts: 5
After max: Move to failed queue
```

**Provider Rotation:**
```
Attempt 1: Provider A
Attempt 2: Provider B (if available)
Attempt 3: Provider C (if available)
Attempt 4: Provider A (retry)

If all providers fail: Backoff and retry
```

### 4.3 Failed Block Queue

**Queue Structure:**
- Block number
- Failure reason
- Failure timestamp
- Retry count
- Error details
- Processing priority

**Processing Rules:**
- Auto-retry up to max attempts
- After max: Require manual review
- Admin can trigger manual retry
- Can skip permanently failed blocks
- Alert on critical failures

**Monitoring:**
- Failed block count metric
- Alert when count > threshold
- Dashboard showing failed blocks
- Ability to bulk retry

---

## 5. Cursor Management

### 5.1 Cursor State Machine

```
States:
- INIT: Starting state, no cursor
- SCANNING: Normal forward progress
- CATCHUP: Behind, moving fast
- BACKFILL: Processing historical gaps
- PAUSED: Stopped by operator
- REORG: Rolling back due to reorg
```

### 5.2 Cursor Storage

**Per Chain Cursor:**
```
Fields:
- chain_id
- current_block
- current_block_hash
- last_update_timestamp
- state (scanning/catchup/etc)
- metadata (JSON)
```

**Metadata Examples:**
```
{
  "provider_used": "alchemy",
  "processing_speed": "2 blocks/sec",
  "last_reorg_at": "2025-01-10T12:00:00Z",
  "missing_blocks_count": 5
}
```

### 5.3 Cursor Update Rules

**Rule 1: Atomic Update**
```
Only update cursor AFTER:
- Block fully processed
- All events emitted successfully
- All downstream confirmations received

NEVER update cursor if any step fails
```

**Rule 2: Finality Consideration**
```
Option A: Update immediately (fast but may reorg)
Option B: Update after finality (slow but safe)

Recommendation: 
- Update immediately for progress tracking
- Mark as "finalized" after N confirmations
```

**Rule 3: Reorg Safety**
```
Always store block hash with cursor:
- Enables reorg detection
- Verification on restart
- Consistency check
```

---

## 6. RPC Budget Management System

### 6.1 Budget Tracking

**Per Provider Tracking:**
```
Metrics:
- Total calls today
- Calls per hour (current)
- Projected daily total
- Quota limit
- Remaining quota
- Quota reset time
```

**Per Chain Allocation:**
```
Budget distribution:
- Chain A: 40% of daily quota
- Chain B: 30%
- Chain C: 20%
- Reserve: 10% (for missing blocks, reorgs)
```

### 6.2 Adaptive Throttling

**Normal Operation (0-50% quota used):**
- Scan interval: 2 seconds
- Batch size: 10 blocks
- All chains enabled

**Warning Zone (50-70% quota used):**
- Scan interval: 3 seconds
- Batch size: 5 blocks
- Log warnings

**Critical Zone (70-90% quota used):**
- Scan interval: 5 seconds
- Batch size: 3 blocks
- Disable low-priority chains
- Pause backfill operations

**Emergency Mode (90-100% quota used):**
- Scan interval: 30 seconds
- Process only highest priority chain
- Disable all background tasks
- Alert operators
- Switch to backup provider if available

### 6.3 Quota Reset Handling

**Daily Reset:**
```
At quota reset time (usually midnight UTC):
- Reset all counters
- Resume normal operation
- Re-enable paused chains
- Process backlog from queue
```

**Provider Rotation on Limit:**
```
Provider A hits limit:
- Switch to Provider B
- Continue operation
- Mark Provider A quota as exhausted
- Wait for reset
```

---

## 7. Health Monitoring System

### 7.1 System Health Indicators

**Critical Metrics:**
- Current block lag (current - latest)
- Missing blocks count
- Failed blocks count
- RPC error rate (last hour)
- Event emission success rate
- Cursor update frequency

**Health States:**
```
HEALTHY:
- Lag < 10 blocks
- No missing blocks
- RPC error rate < 1%
- All events emitting

DEGRADED:
- Lag 10-100 blocks
- Missing blocks < 10
- RPC error rate 1-5%
- Some event delays

CRITICAL:
- Lag > 100 blocks
- Missing blocks > 10
- RPC error rate > 5%
- Event emission failing
```

### 7.2 Alerting Rules

**Alert Level 1: Warning**
- RPC quota at 70%
- Block lag > 50
- 1 reorg detected

**Alert Level 2: Critical**
- RPC quota at 90%
- Block lag > 200
- Multiple reorgs (>3 in hour)
- Event emission failures

**Alert Level 3: Emergency**
- RPC quota exhausted
- Block lag > 1000
- All providers down
- Cursor corruption detected

### 7.3 Health Check Endpoints

**API Endpoints:**
```
/health
- Returns: 200 OK if healthy
- Returns: 503 if critical

/health/detailed
- Current lag per chain
- Missing blocks count
- RPC quota usage
- Last successful block
- Provider status

/health/metrics
- Prometheus format
- All metrics for monitoring
```

---

## 8. Data Consistency Guarantees

### 8.1 Idempotency

**Principle:** Same block processed multiple times = same result

**Implementation:**
- Transaction ID = unique key
- Database upsert (not insert)
- Event deduplication in emitter
- Downstream services handle duplicates

### 8.2 Ordering Guarantees

**Block Level:**
- Blocks processed in order
- Never process block N+1 before block N
- Exception: Parallel backfill (separate mode)

**Event Level:**
- Events emitted in block order
- Events within block maintain TX order
- Reorg events sent before replacement events

### 8.3 Completeness Guarantees

**What is guaranteed:**
- No blocks skipped (detected and backfilled)
- No transactions missed within processed blocks
- All matching addresses captured

**What is NOT guaranteed:**
- Real-time processing (may lag)
- Instant reorg detection (some delay acceptable)
- 100% uptime (graceful degradation)

---

## 9. Configuration & Tuning

### 9.1 Global Configuration

**RPC Settings:**
- Timeout per call
- Max retries
- Backoff strategy
- Batch support enabled/disabled
- Rate limit per provider

**Performance:**
- Scan interval (normal/catchup)
- Batch size per mode
- Parallel workers (for backfill)
- Queue sizes

**Reliability:**
- Finality confirmations required
- Reorg check depth
- Health check interval
- Missing block scan interval

### 9.2 Per-Chain Configuration

**Chain Specifics:**
- Network ID
- RPC endpoints (multiple)
- Optimization strategy (bloom/native/utxo/sender)
- Finality blocks
- Expected block time
- Priority (1-10)

**Budget Allocation:**
- Daily RPC quota percentage
- Emergency reserve percentage
- Backfill quota

**Features:**
- Reorg detection enabled/disabled
- Missing block detection enabled/disabled
- Event types to track
- Address list source

### 9.3 Runtime Tuning

**Dynamic Adjustment:**
- Scan interval based on lag
- Batch size based on RPC performance
- Provider selection based on health
- Feature enabling/disabling based on quota

**Admin Controls:**
- Pause/resume chain
- Force reorg check
- Trigger backfill
- Clear failed queue
- Reset cursor

---

## 10. Operational Procedures

### 10.1 Initial Deployment

**Steps:**
1. Configure chains and RPC providers
2. Set initial cursor (genesis or specific block)
3. Load tracked addresses
4. Start in catchup mode
5. Monitor until current
6. Switch to normal mode

**Considerations:**
- Historical backfill may take days/weeks
- Use paid RPC for initial sync
- Can skip old blocks if not needed

### 10.2 Adding New Chain

**Steps:**
1. Add chain configuration
2. Implement chain adapter (if new type)
3. Configure RPC providers
4. Set budget allocation
5. Deploy with new chain disabled
6. Test with low block range
7. Enable chain
8. Monitor performance

**Zero Impact:**
- No changes to existing chains
- No core code changes needed
- Independent cursor and queue

### 10.3 Provider Changes

**Adding Provider:**
1. Add to config
2. Test connectivity
3. Deploy with low weight
4. Monitor performance
5. Increase weight if stable

**Removing Provider:**
1. Set weight to 0
2. Drain existing requests
3. Remove from config
4. Deploy

**Provider Failure:**
- Automatic circuit breaker
- Rotation to other providers
- No manual intervention needed

### 10.4 Reorg Recovery

**Manual Procedure:**
1. Stop indexer
2. Identify reorg depth
3. Truncate affected blocks from DB
4. Reset cursor to safe point
5. Clear cache
6. Restart indexer
7. Verify re-indexing
8. Emit correction events

**Automated Procedure:**
- System detects reorg
- Automatically rolls back
- Re-indexes correct chain
- Emits revert + new events
- No manual intervention

### 10.5 Disaster Recovery

**Database Loss:**
1. Restore from backup
2. Check last cursor position
3. Mark all blocks after cursor as missing
4. Run backfill from cursor to current
5. Verify data completeness

**Cursor Corruption:**
1. Fetch latest block from chain
2. Compare with DB latest block
3. If mismatch: verify backwards to find safe point
4. Reset cursor to safe point
5. Backfill gap

**Complete Failure:**
1. Deploy fresh instance
2. Set cursor to acceptable starting point
3. Accept data loss before that point
4. Run forward from cursor

---

## 11. Performance Optimization

### 11.1 Database Optimization

**Indexing Strategy:**
- Index on block_number for range queries
- Index on tx_hash for lookups
- Index on from_address and to_address for filtering
- Partition by chain_id
- Partition by date (for old data archival)

**Query Optimization:**
- Batch inserts (100-1000 rows)
- Connection pooling
- Prepared statements
- Read replicas for queries

### 11.2 Memory Optimization

**Bloom Filter:**
- Keep in memory for fast lookup
- Rebuild periodically from DB
- Size based on address count

**Caching:**
- Cache recent blocks (last 100)
- Cache address list
- Cache provider health status
- TTL-based expiration

**Queue Management:**
- Limit queue size in memory
- Overflow to database
- Process in batches

### 11.3 Network Optimization

**HTTP Optimization:**
- Keep-alive connections
- Connection pooling
- HTTP/2 if supported
- Compression

**RPC Optimization:**
- Batch calls when possible
- Parallel requests to different providers
- Request pipelining
- Caching responses (for immutable blocks)

---

## 12. Monitoring & Observability

### 12.1 Metrics to Track

**Indexing Metrics:**
- blocks_processed_total
- chain_latest_block
- indexer_latest_block

**RPC Metrics:**
- rpc_calls_total (by chain, provider, method)
- rpc_errors_total (by error type)
- rpc_latency_seconds (percentiles)

### 12.2 Logging Strategy

**Log Levels:**
```
DEBUG: Every block fetch, every RPC call
INFO: Block processed, cursor updated
WARN: RPC errors, retries, quota warnings
ERROR: Failed blocks, reorg detected, critical errors
```

**Structured Logging:**
- Always include: chain_id, block_number, timestamp
- Include context: provider, method, latency
- Include trace IDs for request tracking

### 12.3 Dashboards

**Operations Dashboard:**
- Current lag per chain
- RPC quota usage (gauge)
- Event emission rate (graph)
- Error rate (graph)
- Provider health (status)

**Performance Dashboard:**
- Blocks per second (graph)
- RPC latency (percentiles)
- Event emission latency
- Queue depths

**Alerts Dashboard:**
- Active alerts
- Recent reorgs
- Failed blocks list
- Missing blocks list

---

## 13. Security Considerations

### 13.1 RPC Key Management

**Best Practices:**
- Store keys in environment variables or secret manager
- Rotate keys periodically
- Different keys per chain if possible
- Monitor for key leakage

**Multiple Keys:**
- Use different keys for different environments (dev/staging/prod)
- Separate keys for backfill vs real-time
- Emergency backup keys

### 13.2 Access Control

**API Security:**
- Authentication for admin endpoints
- Rate limiting on public endpoints
- IP whitelisting for sensitive operations

**Database Security:**
- Principle of least privilege
- Read-only replicas for queries
- Write access only for indexer service

### 13.3 Data Validation

**Input Validation:**
- Validate block numbers are sequential
- Validate block hashes are valid hex
- Validate addresses are valid format
- Validate amounts are non-negative

**Output Validation:**
- Verify events match schema
- Check for impossible values
- Validate confirmations count

---

## 14. Testing Strategy

### 14.1 Unit Testing

**Components to Test:**
- Bloom filter logic
- Address matching
- Cursor management
- Reorg detection
- Retry logic

**Mock Dependencies:**
- Mock RPC providers
- Mock database
- Mock event emitter

### 14.2 Integration Testing

**Test Scenarios:**
- Normal block processing
- Missing block detection and backfill
- Reorg detection and recovery
- RPC provider failover
- Quota exhaustion handling

**Test Data:**
- Use testnet chains
- Use historical mainnet blocks
- Synthetic test cases

### 14.3 Load Testing

**Scenarios:**
- High transaction volume blocks
- Multiple reorgs in sequence
- All providers slow/down
- Database slow
- Event emitter backpressure

**Metrics:**
- Throughput degradation
- Memory usage
- Error rates
- Recovery time

---

## 15. Deployment Strategy

### 15.1 Blue-Green Deployment

**Process:**
1. Deploy new version (green)
2. Run parallel with old version (blue)
3. Compare outputs for consistency
4. Switch traffic to green
5. Keep blue as backup
6. Decommission blue after verification

### 15.2 Rolling Update

**Process:**
1. Deploy to one instance
2. Monitor for errors
3. If stable: deploy to next instance
4. Repeat until all updated
5. Rollback if issues detected

### 15.3 Canary Deployment

**Process:**
1. Deploy new version for one chain only
2. Monitor metrics vs other chains
3. If stable: expand to more chains
4. Full rollout after validation

---

## 16. Scaling Strategy

### 16.1 Vertical Scaling

**When to Scale Up:**
- CPU usage > 70%
- Memory usage > 80%
- Database connections maxed
- Network bandwidth saturated

**How to Scale:**
- Increase instance size
- More database connections
- Larger connection pools
- More memory for caching

### 16.2 Horizontal Scaling (Limited)

**Approach:**
- One indexer instance per chain
- Each instance has own RPC keys
- Each instance has own database (or schema)
- No shared state between instances

**NOT Recommended:**
- Multiple instances for same chain
- Reason: Cursor conflicts, duplicate events

### 16.3 Read Scaling

**Strategy:**
- Index once, read many
- Read replicas for query API
- Cache layer for common queries
- CDN for static data
