package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"

	"mem-sys/internal/db"
	"mem-sys/internal/mcp"
	memhttp "mem-sys/internal/http"
	"mem-sys/internal/service"
	"mem-sys/internal/store"
)

func main() {
	dbPath := flag.String("db", "mem-sys.db", "SQLite database path")
	httpAddr := flag.String("http", "localhost:8080", "HTTP listen address")
	mcpMode := flag.Bool("mcp", false, "Run as MCP stdio server")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	d, err := db.Open(*dbPath)
	if err != nil {
		log.Error("open db", "err", err)
		os.Exit(1)
	}
	defer d.Close()

	st := store.New(d)
	svc := service.New(st, service.Config{AdminOverrideRequiresReason: true})

	ctx := context.Background()
	if err := seed(ctx, svc); err != nil {
		log.Warn("seed", "err", err)
	}

	if *mcpMode {
		mcpSrv := mcp.New(svc)
		if err := mcpSrv.Run(); err != nil {
			log.Error("mcp", "err", err)
			os.Exit(1)
		}
		return
	}

	h := memhttp.New(svc)
	mux := h.Handler()
	log.Info("listening", "addr", *httpAddr)
	if err := http.ListenAndServe(*httpAddr, mux); err != nil {
		log.Error("http", "err", err)
		os.Exit(1)
	}
}

func seed(ctx context.Context, svc *service.Service) error {
	_ = svc.St().CreatePrincipal(ctx, store.Principal{ID: "admin-alice", Name: "Alice", Type: "human", GlobalRole: "admin"})
	_ = svc.St().CreatePrincipal(ctx, store.Principal{ID: "user-bob", Name: "Bob", Type: "human", GlobalRole: "user"})
	_ = svc.St().CreatePrincipal(ctx, store.Principal{ID: "agent-dev", Name: "dev-agent", Type: "agent", GlobalRole: "user"})

	if _, err := svc.St().GetModuleByName(ctx, "auth"); err != nil {
		mod := store.Module{Name: "auth", Description: "Authentication module", Status: "active", CreatedBy: "admin-alice"}
		if err := svc.St().CreateModule(ctx, mod); err != nil {
			return err
		}
		m, _ := svc.St().GetModuleByName(ctx, "auth")
		_ = svc.St().GrantModuleOwner(ctx, store.ModuleOwnerGrant{ModuleID: m.ID, PrincipalID: "admin-alice", GrantedBy: "admin-alice"})
	}
	return nil
}
