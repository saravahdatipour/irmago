// Executable for the irmaserver.
package main

import (
	"encoding/json"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/go-errors/errors"
	"github.com/privacybydesign/irmago/server"
	"github.com/privacybydesign/irmago/server/irmaserver"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var logger = logrus.StandardLogger()
var conf *irmaserver.Configuration

var RootCommand = &cobra.Command{
	Use:   "irmad",
	Short: "IRMA server for verifying and issuing attributes",
	Run: func(command *cobra.Command, args []string) {
		if err := configure(command); err != nil {
			die(errors.WrapPrefix(err, "Failed to configure server", 0))
		}
		if err := irmaserver.Start(conf); err != nil {
			die(errors.WrapPrefix(err, "Failed to start server", 0))
		}
	},
}

var RunCommand = &cobra.Command{
	Use:   "run",
	Short: "Run server (same as specifying no command)",
	Run:   RootCommand.Run,
}

var CheckCommand = &cobra.Command{
	Use:   "check",
	Short: "Check server configuration correctness",
	Long: `check reads the server configuration like the main command does, from a
configuration file, command line flags, or environmental variables, and checks
that the configuration is valid.

Specify -v to see the configuration.`,
	Run: func(command *cobra.Command, args []string) {
		if err := configure(command); err != nil {
			die(errors.WrapPrefix(err, "Failed to read configuration from file, args, or env vars", 0))
		}
		conf.SchemeUpdateInterval = 0
		if err := irmaserver.Initialize(conf); err != nil {
			die(errors.WrapPrefix(err, "Invalid configuration", 0))
		}
	},
}

func main() {
	logger.Level = logrus.InfoLevel
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	RootCommand.AddCommand(CheckCommand, RunCommand)

	for _, cmd := range []*cobra.Command{RootCommand, CheckCommand, RunCommand} {
		if err := setFlags(cmd); err != nil {
			die(errors.WrapPrefix(err, "Failed to attach flags to "+cmd.Name()+" command", 0))
		}
	}

	if err := RootCommand.Execute(); err != nil {
		die(errors.WrapPrefix(err, "Failed to execute command", 0))
	}
}

func die(err *errors.Error) {
	msg := err.Error()
	if logger.IsLevelEnabled(logrus.TraceLevel) {
		msg += "\nStack trace:\n" + string(err.Stack())
	}
	logger.Fatal(msg)
}

func setFlags(cmd *cobra.Command) error {
	flags := cmd.Flags()
	flags.SortFlags = false

	cachepath, err := server.CachePath()
	if err != nil {
		return err
	}
	defaulturl, err := server.LocalIP()
	if err != nil {
		logger.Warn("Could not determine local IP address: ", err.Error())
	} else {
		defaulturl = "http://" + defaulturl + ":port"
	}

	flags.StringP("config", "c", "", "Path to configuration file")
	flags.StringP("irmaconf", "i", "", "path to irma_configuration")
	flags.String("cachepath", cachepath, "Directory for writing cache files to")
	flags.Uint("schemeupdate", 60, "Update IRMA schemes every x minutes (0 to disable)")
	flags.Int("maxrequestage", 300, "Max age in seconds of a session request JWT")
	flags.StringP("url", "u", defaulturl, "External URL to server to which the IRMA client connects")

	flags.StringP("listenaddr", "l", "0.0.0.0", "Address at which to listen")
	flags.IntP("port", "p", 8088, "Port at which to listen")
	flags.Int("clientport", 0, "If specified, start a separate server for the IRMA app at his port")
	flags.String("clientlistenaddr", "", "Address at which server for IRMA app listens")
	flags.Lookup("listenaddr").Header = `Server address and port to listen on. If the client* configuration options are provided (see also the TLS flags)
then the endpoints at /session for the requestor and /irma for the irmaclient (i.e. IRMA app) will listen on
distinct network endpoints (e.g., localhost:1234/session and 0.0.0.0:5678/irma).`

	flags.Bool("noauth", false, "Whether or not to authenticate requestors")
	flags.String("requestors", "", "Requestor configuration (in JSON)")
	flags.Lookup("noauth").Header = `Requestor authentication. If disabled, then anyone that can reach this server can submit requests to it.
If it is enabled, then requestor specific configuration must be provided.`

	flags.StringSlice("disclose", nil, "list of attributes that all requestors may verify (default *)")
	flags.StringSlice("sign", nil, "list of attributes that all requestors may request in signatures (default *)")
	flags.StringSlice("issue", nil, "list of attributes that all requestors may issue")
	flags.Lookup("disclose").Header = `Default requestor permissions. These apply to all requestors, in addition to any permissions a requestor may
have specifically. May contain wildcards. Separate multiple with comma. Example: irma-demo.*,pbdf.*
By default all requestors may use all attributes in disclosure and signature sessions.
Pass empty string to disable session type.`

	flags.StringP("privatekeys", "k", "", "path to IRMA private keys")
	flags.Lookup("privatekeys").Header = `Path to a folder containing IRMA private keys, with filenames scheme.issuer.xml, e.g. irma-demo.MijnOverheid.xml.
Private keys may also be stored in the scheme (e.g. irma-demo/MijnOverheid/PrivateKeys/0.xml).`

	flags.StringP("jwtissuer", "j", "irmaserver", "JWT issuer")
	flags.String("jwtprivatekey", "", "JWT private key")
	flags.String("jwtprivatekeyfile", "", "Path to JWT private key")
	flags.Lookup("jwtissuer").Header = `JWT configuration. Can be omitted but then endpoints that return signed JWTs are disabled.
All of the keys and certificates below are expected in PEM. Pass it either directly, or a path to it
using the corresponding "-file" flag.`

	flags.String("tlscertificate", "", "TLS certificate")
	flags.String("tlscertificatefile", "", "Path to TLS certificate ")
	flags.String("tlsprivatekey", "", "TLS private key")
	flags.String("tlsprivatekeyfile", "", "Path to TLS private key")
	flags.String("clienttlscertificate", "", "TLS certificate for IRMA app server")
	flags.String("clienttlscertificatefile", "", "Path to TLS certificate for IRMA app server")
	flags.String("clienttlsprivatekey", "", "TLS private key for IRMA app server")
	flags.String("clienttlsprivatekeyfile", "", "Path to TLS private key for IRMA app server")
	flags.Lookup("tlscertificate").Header = "TLS configuration. Leave empty to disable TLS."

	flags.CountP("verbose", "v", "verbose (repeatable)")
	flags.BoolP("quiet", "q", false, "quiet")
	flags.Lookup("verbose").Header = `Other options`

	return nil
}

func configure(cmd *cobra.Command) error {
	viper.SetEnvPrefix("IRMASERVER")
	viper.AutomaticEnv()
	if err := viper.BindPFlags(cmd.Flags()); err != nil {
		return err
	}

	// Locate and read configuration file
	confpath := viper.GetString("config")
	if confpath != "" {
		dir, file := filepath.Dir(confpath), filepath.Base(confpath)
		viper.SetConfigName(strings.TrimSuffix(file, filepath.Ext(file)))
		viper.AddConfigPath(dir)
	} else {
		viper.SetConfigName("irmaserver")
		viper.AddConfigPath(".")
		viper.AddConfigPath("/etc/irmaserver/")
		viper.AddConfigPath("$HOME/.irmaserver")
	}
	err := viper.ReadInConfig() // Hold error checking until we know how much of it to log

	// Set log level
	logger.Level = server.Verbosity(viper.GetInt("verbose"))
	if viper.GetBool("quiet") {
		logger.Out = ioutil.Discard
	}

	logger.Debug("Configuring")
	logger.Debug("Log level: ", logger.Level.String())
	if err != nil {
		if _, notfound := err.(viper.ConfigFileNotFoundError); notfound {
			logger.Info("No configuration file found")
		} else {
			die(errors.WrapPrefix(err, "Failed to unmarshal configuration file at "+viper.ConfigFileUsed(), 0))
		}
	} else {
		logger.Info("Config file: ", viper.ConfigFileUsed())
	}

	// Read configuration from flags and/or environmental variables
	conf = &irmaserver.Configuration{
		Configuration: &server.Configuration{
			IrmaConfigurationPath: viper.GetString("irmaconf"),
			IssuerPrivateKeysPath: viper.GetString("privatekeys"),
			CachePath:             viper.GetString("cachepath"),
			URL:                   viper.GetString("url"),
			SchemeUpdateInterval:  viper.GetInt("schemeupdate"),
			Logger:                logger,
		},
		Permissions: irmaserver.Permissions{
			Disclosing: handlePermission("disclose"),
			Signing:    handlePermission("sign"),
			Issuing:    viper.GetStringSlice("issue"),
		},
		ListenAddress:                  viper.GetString("listenaddr"),
		Port:                           viper.GetInt("port"),
		ClientListenAddress:            viper.GetString("clientlistenaddr"),
		ClientPort:                     viper.GetInt("clientport"),
		DisableRequestorAuthentication: viper.GetBool("noauth"),
		Requestors:                     make(map[string]irmaserver.Requestor),
		JwtIssuer:                      viper.GetString("jwtissuer"),
		JwtPrivateKey:                  viper.GetString("jwtprivatekey"),
		JwtPrivateKeyFile:              viper.GetString("jwtprivatekeyfile"),
		MaxRequestAge:                  viper.GetInt("maxrequestage"),
		Verbose:                        viper.GetInt("verbose"),
		Quiet:                          viper.GetBool("quiet"),

		TlsCertificate:           viper.GetString("tlscertificate"),
		TlsCertificateFile:       viper.GetString("tlscertificatefile"),
		TlsPrivateKey:            viper.GetString("tlsprivatekey"),
		TlsPrivateKeyFile:        viper.GetString("tlsprivatekeyfile"),
		ClientTlsCertificate:     viper.GetString("clienttlscertificate"),
		ClientTlsCertificateFile: viper.GetString("clienttlscertificatefile"),
		ClientTlsPrivateKey:      viper.GetString("clienttlsprivatekey"),
		ClientTlsPrivateKeyFile:  viper.GetString("clienttlsprivatekeyfile"),
	}

	// Handle requestors
	if len(viper.GetStringMap("requestors")) > 0 { // First read config file
		if err := viper.UnmarshalKey("requestors", &conf.Requestors); err != nil {
			return errors.WrapPrefix(err, "Failed to unmarshal requestors from config file", 0)
		}
	}
	requestors := viper.GetString("requestors") // Read flag or env var
	if len(requestors) > 0 {
		if err := json.Unmarshal([]byte(requestors), &conf.Requestors); err != nil {
			return errors.WrapPrefix(err, "Failed to unmarshal requestors from json", 0)
		}
	}

	logger.Debug("Done configuring")

	return nil
}

func handlePermission(typ string) []string {
	if !viper.IsSet(typ) {
		return []string{"*"}
	}
	perms := viper.GetStringSlice(typ)
	if perms == nil {
		return []string{}
	}
	return perms
}
