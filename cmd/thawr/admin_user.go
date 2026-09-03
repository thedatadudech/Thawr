package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// envPasswordFile names a file holding the password for user create.
const envPasswordFile = "THAWR_PASSWORD_FILE"

type userJSON struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	Disabled  bool   `json:"disabled"`
	CreatedAt string `json:"created_at"`
}

func newAdminUserCmd(flags *adminFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "user", Short: "Manage local users"}
	var role string
	create := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a user; the password is read from $" + envPasswordFile + " or prompted",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			password, err := readPassword(cmd)
			if err != nil {
				return err
			}
			var u userJSON
			if err := newAdminClient(flags.socket).do(cmd.Context(), "POST", "/api/v1/users",
				map[string]string{"name": args[0], "role": role, "password": password}, &u); err != nil {
				return err
			}
			if flags.json {
				return printJSON(cmd.OutOrStdout(), u)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "user %s created with role %s\n", u.Name, u.Role)
			return err
		},
	}
	create.Flags().StringVar(&role, "role", "member", "admin or member")
	list := &cobra.Command{
		Use:   "list",
		Short: "List users",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var users []userJSON
			if err := newAdminClient(flags.socket).do(cmd.Context(), "GET", "/api/v1/users", nil, &users); err != nil {
				return err
			}
			if flags.json {
				return printJSON(cmd.OutOrStdout(), users)
			}
			rows := make([][]string, 0, len(users))
			for _, u := range users {
				rows = append(rows, []string{u.Name, u.Role, u.CreatedAt})
			}
			return table(cmd.OutOrStdout(), []string{"NAME", "ROLE", "CREATED"}, rows)
		},
	}
	cmd.AddCommand(create, list)
	return cmd
}

// readPassword takes the password from the file named by
// THAWR_PASSWORD_FILE, else prompts twice on a terminal. It is never
// accepted as a flag so it cannot end up in shell history.
func readPassword(cmd *cobra.Command) (string, error) {
	if path := os.Getenv(envPasswordFile); path != "" {
		data, err := os.ReadFile(path) //nolint:gosec // operator-chosen path from the environment
		if err != nil {
			return "", fmt.Errorf("read %s: %w", envPasswordFile, err)
		}
		return strings.TrimRight(string(data), "\r\n"), nil
	}
	in, ok := cmd.InOrStdin().(*os.File)
	if !ok || !term.IsTerminal(int(in.Fd())) {
		return "", errors.New("no terminal for the password prompt; set " + envPasswordFile + " to a file containing the password")
	}
	_, _ = fmt.Fprint(cmd.ErrOrStderr(), "Password: ")
	first, err := term.ReadPassword(int(in.Fd()))
	_, _ = fmt.Fprintln(cmd.ErrOrStderr())
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	_, _ = fmt.Fprint(cmd.ErrOrStderr(), "Repeat password: ")
	second, err := term.ReadPassword(int(in.Fd()))
	_, _ = fmt.Fprintln(cmd.ErrOrStderr())
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	if string(first) != string(second) {
		return "", errors.New("passwords do not match")
	}
	return string(first), nil
}
