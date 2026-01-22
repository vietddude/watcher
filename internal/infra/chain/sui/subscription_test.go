package sui

import (
	"context"
	"crypto/tls"
	"fmt"
	"testing"
	"time"

	v2 "github.com/vietddude/watcher/internal/infra/chain/sui/generated/sui/rpc/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func TestSubscribeCheckpoints(t *testing.T) {
	// Use the public mainnet endpoint which supports gRPC
	target := "api-sui-mainnet-full.n.dwellir.com:443"

	// Create a connection with TLS credentials
	creds := credentials.NewTLS(&tls.Config{})
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(creds))
	if err != nil {
		t.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	client := v2.NewSubscriptionServiceClient(conn)

	// Context with timeout to ensure the test finishes
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Subscribe to checkpoints with ReadMask
	mask := &fieldmaskpb.FieldMask{
		Paths: []string{
			"checkpoint.sequence_number",
			"checkpoint.digest",
			"checkpoint.summary",
			"checkpoint.content",
			"checkpoint.transactions",
		},
	}

	req := &v2.SubscribeCheckpointsRequest{
		ReadMask: mask,
	}
	stream, err := client.SubscribeCheckpoints(ctx, req)
	if err != nil {
		t.Fatalf("SubscribeCheckpoints failed: %v", err)
	}

	fmt.Println("Subscribed to checkpoints. Waiting for data...")

	count := 0
	maxCheckpoints := 5

	for {
		resp, err := stream.Recv()
		if err != nil {
			t.Fatalf("stream.Recv failed: %v", err)
		}

		if resp.Checkpoint != nil {
			checkpoint := resp.Checkpoint
			seq := checkpoint.GetSequenceNumber()
			cursor := resp.GetCursor()

			if seq == 0 {
				t.Logf("Warning: Received sequence number 0. Cursor: %d", cursor)
			}
			if cursor > 0 {
				t.Logf("Success: Received Cursor %d (SequenceNumber: %d, Transactions: %d)", cursor, seq, len(checkpoint.GetTransactions()))
			}

			if checkpoint.GetDigest() != "" {
				t.Logf("Digest: %s", checkpoint.GetDigest())
			}
			if checkpoint.GetSummary() != nil {
				t.Logf("Has Summary: true")
			}
			if len(checkpoint.GetTransactions()) > 0 {
				t.Logf("Transactions Count: %d", len(checkpoint.GetTransactions()))
				if len(checkpoint.GetTransactions()) > 0 {
					t.Logf("Sample Transaction Digest: %s", checkpoint.GetTransactions()[0].GetTransaction().GetDigest())
				}
			}

			count++
			if count >= maxCheckpoints {
				t.Log("Received enough checkpoints, finishing test.")
				return
			}
		} else {
			fmt.Println("Received response without checkpoint data")
		}

		count++
		if count >= maxCheckpoints {
			fmt.Println("Received enough checkpoints, finishing test.")
			break
		}
	}
}
