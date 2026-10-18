package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/access"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/featureflags"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/settings"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache/memory"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/seeder"
)

func registerSeedCommands(registry *Registry, state *State) error {
	commands := []*cobra.Command{
		newPermissionSyncCommand(state),
		newDBSeedCommand(state),
	}
	for _, cmd := range commands {
		if err := registry.Add(cmd); err != nil {
			return err
		}
	}
	return nil
}

func newPermissionSyncCommand(state *State) *cobra.Command {
	return &cobra.Command{
		Use:   "permission:sync",
		Short: "Sync the permission catalog into the database",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			registry, cleanup, err := openDatabase(ctx, state)
			if err != nil {
				return err
			}
			defer cleanup()

			queries := dbgen.New(registry.Primary().Pool)
			store := memory.New(state.Config().Cache.Prefix)
			defer store.Close()

			checker := authz.NewChecker(queries, store)
			service := access.NewPermissionService(access.NewPermissionRepository(queries), checker)
			affected, err := service.SyncCatalog(ctx)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Synced %d permission(s).\n", affected)
			return nil
		},
	}
}

func newDBSeedCommand(state *State) *cobra.Command {
	return &cobra.Command{
		Use:   "db:seed",
		Short: "Seed permissions, tenant/roles/admin, settings groups/globals and feature flags",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			registry, cleanup, err := openDatabase(ctx, state)
			if err != nil {
				return err
			}
			defer cleanup()

			cfg := state.Config()
			primary := registry.Primary()
			queries := dbgen.New(primary.Pool)
			tx := database.NewTxManager(primary)
			store := memory.New(cfg.Cache.Prefix)
			defer store.Close()

			checker := authz.NewChecker(queries, store)
			if err := seeder.Run(ctx, cfg, queries, tx, checker, state.Logger()); err != nil {
				return err
			}
			// Materialize the code-defined settings groups/globals and feature-flag
			// globals so the settings surface is complete after a plain seed.
			if _, err := settings.NewService(settings.NewRepository(queries), store).Sync(ctx); err != nil {
				return err
			}
			if _, err := featureflags.NewService(featureflags.NewRepository(queries), store).Sync(ctx); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Seeding completed.")
			return nil
		},
	}
}
