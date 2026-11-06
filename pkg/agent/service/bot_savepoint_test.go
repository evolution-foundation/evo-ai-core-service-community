package service

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"

	"evo-ai-core-service/pkg/agent/model"
	"evo-ai-core-service/pkg/evoextensions/runtimecontext"
)

// recordingConn logs every statement and fails the ones listed in failOn.
type recordingConn struct {
	log    *[]string
	failOn map[string]bool
}

func (c recordingConn) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	*c.log = append(*c.log, query)
	if c.failOn[query] {
		return nil, errors.New("conn refused " + query)
	}
	return nil, nil
}

func (recordingConn) PrepareContext(context.Context, string) (*sql.Stmt, error) { return nil, nil }

func (recordingConn) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, nil
}

func (recordingConn) QueryRowContext(context.Context, string, ...any) *sql.Row { return nil }

func TestWriteAgentBot(t *testing.T) {
	const (
		open     = "SAVEPOINT " + botSavepoint
		rollback = "ROLLBACK TO SAVEPOINT " + botSavepoint
		release  = "RELEASE SAVEPOINT " + botSavepoint
		write    = "bot write"
	)
	errBot := errors.New("bot refused")

	cases := []struct {
		name       string
		bound      bool
		failOn     string
		writeErr   error
		wantLog    []string
		wantBot    bool
		wantBotErr error
		wantTxErr  string
	}{
		{name: "no bound conn writes directly", writeErr: errBot, wantLog: []string{write}, wantBotErr: errBot},
		{name: "bound success releases", bound: true, wantLog: []string{open, write, release}, wantBot: true},
		{name: "bound bot failure rolls back to the savepoint", bound: true, writeErr: errBot,
			wantLog: []string{open, write, rollback}, wantBotErr: errBot},
		{name: "savepoint refused fails before writing", bound: true, failOn: open,
			wantLog: []string{open}, wantTxErr: "opening"},
		{name: "rollback refused fails the request", bound: true, failOn: rollback, writeErr: errBot,
			wantLog: []string{open, write, rollback}, wantTxErr: "rolling back"},
		{name: "release refused fails the request", bound: true, failOn: release,
			wantLog: []string{open, write, release}, wantTxErr: "releasing"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var log []string
			ctx := context.Background()
			if tc.bound {
				ctx = runtimecontext.WithConn(ctx, recordingConn{log: &log, failOn: map[string]bool{tc.failOn: true}})
			}

			bot, botErr, txErr := writeAgentBot(ctx, func() (*model.AgentBot, error) {
				log = append(log, write)
				if tc.writeErr != nil {
					return nil, tc.writeErr
				}
				return &model.AgentBot{}, nil
			})

			if !reflect.DeepEqual(log, tc.wantLog) {
				t.Errorf("statements = %q, want %q", log, tc.wantLog)
			}
			if (bot != nil) != tc.wantBot {
				t.Errorf("bot returned = %v, want %v", bot != nil, tc.wantBot)
			}
			if !errors.Is(botErr, tc.wantBotErr) || (botErr == nil) != (tc.wantBotErr == nil) {
				t.Errorf("botErr = %v, want %v", botErr, tc.wantBotErr)
			}
			switch {
			case tc.wantTxErr == "" && txErr != nil:
				t.Errorf("unexpected txErr: %v", txErr)
			case tc.wantTxErr != "" && (txErr == nil || !strings.Contains(txErr.Error(), tc.wantTxErr)):
				t.Errorf("txErr = %v, want one mentioning %q", txErr, tc.wantTxErr)
			}
		})
	}
}
