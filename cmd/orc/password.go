package main

import (
	"fmt"

	"github.com/brijbyte/orc/internal/config"
	"github.com/spf13/cobra"
)

func newPasswordCommand() *cobra.Command {
	var rotate bool
	cmd := &cobra.Command{
		Use:   "password",
		Short: "Rotate the web password",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if !rotate {
				return fmt.Errorf("the web password cannot be shown; use --rotate to replace it")
			}
			password, err := config.RotateWebPassword()
			if err != nil {
				return err
			}
			fmt.Println(password)
			return nil
		},
	}
	cmd.Flags().BoolVar(&rotate, "rotate", false, "replace the password and invalidate web sessions")
	return cmd
}
