// pg_goback
//
// Copyright 2020-2021 Nicolas Thauvin. All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
//
//  1. Redistributions of source code must retain the above copyright
//     notice, this list of conditions and the following disclaimer.
//  2. Redistributions in binary form must reproduce the above copyright
//     notice, this list of conditions and the following disclaimer in the
//     documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE AUTHORS ``AS IS'' AND ANY EXPRESS OR
// IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED WARRANTIES
// OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE DISCLAIMED.
// IN NO EVENT SHALL THE AUTHORS OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT,
// INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
// (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES;
// LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND
// ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
// (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF
// THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

package main

import (
	"errors"
	"fmt"
	"github.com/anmitsu/go-shlex"
	"github.com/spf13/pflag"
	"gopkg.in/ini.v1"
	"os"
	"strconv"
	"strings"
	"time"
)

type options struct {
	BinDirectory  string
	Directory     string
	Host          string
	Port          int
	Username      string
	ConnDb        string
	ExcludeDbs    []string
	Dbnames       []string
	WithTemplates bool
	Format        string
	DirJobs       int
	Jobs          int
	PauseTimeout  int
	PurgeInterval time.Duration
	PurgeKeep     int
	SumAlgo       string
	PreHook       string
	PostHook      string
	PgDumpOpts    []string
	PerDbOpts     map[string]*dbOpts
	CfgFile       string
	TimeFormat    string
}

func defaultOptions() options {
	return options{
		Directory:     "/var/backups/postgresql",
		Format:        "custom",
		DirJobs:       1,
		Jobs:          1,
		PauseTimeout:  3600,
		PurgeInterval: -30 * 24 * time.Hour,
		PurgeKeep:     0,
		SumAlgo:       "none",
		CfgFile:       defaultCfgFile,
		TimeFormat:    time.RFC3339,
	}
}

type parseCliResult struct {
	ShowHelp    bool
	ShowVersion bool
}

func (*parseCliResult) Error() string {
	return "parsing of command line args failed"
}

func validateDumpFormat(s string) error {
	for _, v := range []string{"plain", "custom", "tar", "directory"} {
		if strings.HasPrefix(v, s) {
			return nil
		}
	}
	return fmt.Errorf("invalid dump format %q", s)
}

func validatePurgeKeepValue(k string) (int, error) {
	// returning -1 means keep all dumps
	if k == "all" {
		return -1, nil
	}

	keep, err := strconv.ParseInt(k, 10, 0)
	if err != nil {
		// return -1 too when the input is not convertible to an int
		return -1, fmt.Errorf("Invalid input for keep: %w", err)
	}

	if keep < 0 {
		return -1, fmt.Errorf("Invalid input for keep: negative value: %d", keep)
	}

	return int(keep), nil
}

func validatePurgeTimeLimitValue(i string) (time.Duration, error) {
	if days, err := strconv.ParseInt(i, 10, 0); err != nil {
		if errors.Is(err, strconv.ErrRange) {
			return 0, errors.New("Invalid input for purge interval, number too big")
		}
	} else {
		return time.Duration(-days*24) * time.Hour, nil
	}

	d, err := time.ParseDuration(i)
	if err != nil {
		return 0, err
	}
	return -d, nil

}

var defaultCfgFile = "/etc/pg_goback/pg_goback.conf"
var configParseCliInput = os.Args[1:]

func parseCli() (options, []string, error) {
	var purgeKeep, purgeInterval string

	opts := defaultOptions()

	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "pg_goback dumps some PostgreSQL databases\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  pg_goback [OPTION]... [DBNAME]...\n")
		fmt.Fprintf(os.Stderr, "\nOptions:\n")
		pflag.CommandLine.SortFlags = false
		pflag.PrintDefaults()
	}

	pflag.StringVarP(&opts.BinDirectory, "bin-directory", "B", "", "PostgreSQL binaries directory. Empty to search $PATH")
	pflag.StringVarP(&opts.Directory, "backup-directory", "b", "/var/backups/postgresql", "store dump files there")
	pflag.StringVarP(&opts.CfgFile, "config", "c", defaultCfgFile, "alternate config file")
	pflag.StringSliceVarP(&opts.ExcludeDbs, "exclude-dbs", "D", []string{}, "list of databases to exclude")
	pflag.BoolVarP(&opts.WithTemplates, "with-templates", "t", false, "include templates")
	WithoutTemplates := pflag.Bool("without-templates", false, "force exclude templates")
	pflag.IntVarP(&opts.PauseTimeout, "pause-timeout", "T", 3600, "abort if replication cannot be paused after this number of seconds")
	pflag.IntVarP(&opts.Jobs, "jobs", "j", 1, "dump this many databases concurrently")
	pflag.StringVarP(&opts.Format, "format", "F", "custom", "database dump format: plain, custom, tar or directory")
	pflag.IntVarP(&opts.DirJobs, "parallel-backup-jobs", "J", 1, "number of parallel jobs to dumps when using directory format")
	pflag.StringVarP(&opts.SumAlgo, "checksum-algo", "S", "none", "signature algorithm: none sha1 sha224 sha256 sha384 sha512")
	pflag.StringVarP(&purgeInterval, "purge-older-than", "P", "30", "purge backups older than this duration in days\nuse an interval with units \"s\" (seconds), \"m\" (minutes) or \"h\" (hours)\nfor less than a day.")
	pflag.StringVarP(&purgeKeep, "purge-min-keep", "K", "0", "minimum number of dumps to keep when purging or 'all' to keep everything\n")
	pflag.StringVar(&opts.PreHook, "pre-backup-hook", "", "command to run before taking dumps")
	pflag.StringVar(&opts.PostHook, "post-backup-hook", "", "command to run after taking dumps")
	pflag.StringVarP(&opts.Host, "host", "h", "", "database server host or socket directory")
	pflag.IntVarP(&opts.Port, "port", "p", 0, "database server port number")
	pflag.StringVarP(&opts.Username, "username", "U", "", "connect as specified database user")
	pflag.StringVarP(&opts.ConnDb, "dbname", "d", "", "connect to database name\n")

	helpF := pflag.BoolP("help", "?", false, "print usage")
	versionF := pflag.BoolP("version", "V", false, "print version")

	// Do not use the default pflag.Parse() that use os.Args[1:],
	// but pass it explicitly so that unit-tests can feed any set
	// of flags
	pflag.CommandLine.Parse(configParseCliInput)

	// Record the list of flags set on the command line to allow
	// overriding the configuration later, if an alternate
	// configuration file has been provided
	changed := make([]string, 0)
	pflag.Visit(func(f *pflag.Flag) {
		changed = append(changed, f.Name)
	})

	// To override with_templates = true on the command line and
	// make it false, we have to ensure MergeCliAndConfigOptions()
	// use the cli value
	if *WithoutTemplates {
		opts.WithTemplates = false
		changed = append(changed, "with-templates")
	}

	// When --help or --version is given print and tell the caller
	// through the error
	if *helpF {
		pflag.Usage()
		return opts, changed, &parseCliResult{true, false}
	}

	if *versionF {
		fmt.Printf("pg_goback version %v\n", version)
		return opts, changed, &parseCliResult{false, true}
	}

	opts.Dbnames = pflag.Args()

	// When a list of databases have been provided ensure it will
	// override the one from the configuration when options are
	// merged
	if len(opts.Dbnames) > 0 {
		changed = append(changed, "include-dbs")
	}

	// Validate purge keep and time limit
	keep, err := validatePurgeKeepValue(purgeKeep)
	if err != nil {
		return opts, changed, err
	}
	opts.PurgeKeep = keep

	interval, err := validatePurgeTimeLimitValue(purgeInterval)
	if err != nil {
		return opts, changed, err
	}
	opts.PurgeInterval = interval

	return opts, changed, nil
}

func loadConfigurationFile(path string) (options, error) {
	var purgeKeep, purgeInterval string

	opts := defaultOptions()

	cfg, err := ini.Load(path)
	if err != nil {
		return opts, fmt.Errorf("Could load configuration file: %v", err)
	}

	s, _ := cfg.GetSection(ini.DefaultSection)

	// Read all configuration parameters ensuring the destination
	// struct member has the same default value as the commandline
	// flags
	opts.BinDirectory = s.Key("bin_directory").MustString("")
	opts.Directory = s.Key("backup_directory").MustString("/var/backups/postgresql")
	timeFormat := s.Key("timestamp_format").MustString("rfc3339")
	opts.Host = s.Key("host").MustString("")
	opts.Port = s.Key("port").MustInt(0)
	opts.Username = s.Key("user").MustString("")
	opts.ConnDb = s.Key("dbname").MustString("")
	opts.ExcludeDbs = s.Key("exclude_dbs").Strings(",")
	opts.Dbnames = s.Key("include_dbs").Strings(",")
	opts.WithTemplates = s.Key("with_templates").MustBool(false)
	opts.Format = s.Key("format").MustString("custom")
	opts.DirJobs = s.Key("parallel_backup_jobs").MustInt(1)
	opts.Jobs = s.Key("jobs").MustInt(1)
	opts.PauseTimeout = s.Key("pause_timeout").MustInt(3600)
	purgeInterval = s.Key("purge_older_than").MustString("30")
	purgeKeep = s.Key("purge_min_keep").MustString("0")
	opts.SumAlgo = s.Key("checksum_algorithm").MustString("none")
	opts.PreHook = s.Key("pre_backup_hook").MustString("")
	opts.PostHook = s.Key("post_backup_hook").MustString("")

	// Validate purge keep and time limit
	keep, err := validatePurgeKeepValue(purgeKeep)
	if err != nil {
		return opts, err
	}
	opts.PurgeKeep = keep

	interval, err := validatePurgeTimeLimitValue(purgeInterval)
	if err != nil {
		return opts, err
	}
	opts.PurgeInterval = interval

	// Validate the value of the timestamp format
	switch timeFormat {
	case "legacy":
		opts.TimeFormat = "2006-01-02_15-04-05"
	case "rfc3339":
	default:
		return opts, fmt.Errorf("unknown timestamp format: %s", timeFormat)
	}

	// Parse the pg_dump options as a list of args
	words, err := shlex.Split(s.Key("pg_dump_options").String(), true)
	if err != nil {
		return opts, fmt.Errorf("unable to parse pg_dump_options: %w", err)
	}
	opts.PgDumpOpts = words

	// Process all sections with database specific configuration,
	// fallback on the values of the global section
	subs := cfg.Sections()
	opts.PerDbOpts = make(map[string]*dbOpts, len(subs))

	for _, s := range subs {
		if s.Name() == ini.DefaultSection {
			continue
		}

		var dbPurgeInterval, dbPurgeKeep string

		o := dbOpts{}
		o.Format = s.Key("format").MustString(opts.Format)
		o.Jobs = s.Key("parallel_backup_jobs").MustInt(opts.DirJobs)
		o.SumAlgo = s.Key("checksum_algorithm").MustString(opts.SumAlgo)
		dbPurgeInterval = s.Key("purge_older_than").MustString(purgeInterval)
		dbPurgeKeep = s.Key("purge_min_keep").MustString(purgeKeep)

		// Validate purge keep and time limit
		keep, err := validatePurgeKeepValue(dbPurgeKeep)
		if err != nil {
			return opts, err
		}
		o.PurgeKeep = keep

		interval, err := validatePurgeTimeLimitValue(dbPurgeInterval)
		if err != nil {
			return opts, err
		}
		o.PurgeInterval = interval

		o.Schemas = parseIdentifierList(s.Key("schemas").String())
		o.ExcludedSchemas = parseIdentifierList(s.Key("exclude_schemas").String())
		o.Tables = parseIdentifierList(s.Key("tables").String())
		o.ExcludedTables = parseIdentifierList(s.Key("exclude_tables").String())

		if s.HasKey("pg_dump_options") {
			words, err := shlex.Split(s.Key("pg_dump_options").String(), true)
			if err != nil {
				return opts, fmt.Errorf("unable to parse pg_dump_options for %s: %w", s.Name(), err)
			}
			o.PgDumpOpts = words
		} else {
			o.PgDumpOpts = opts.PgDumpOpts
		}

		opts.PerDbOpts[s.Name()] = &o
	}

	return opts, nil
}

func parseIdentifierList(rawList string) []string {
	ids := make([]string, 0)
	if len(strings.TrimSpace(rawList)) > 0 {
		for _, t := range strings.Split(rawList, ";") {
			ids = append(ids, strings.TrimSpace(t))
		}
	}
	return ids
}

func mergeCliAndConfigOptions(cliOpts options, configOpts options, onCli []string) options {
	opts := configOpts

	// Command line values take precedence on everything, including per
	// database options
	for _, o := range onCli {
		switch o {
		case "bin-directory":
			opts.BinDirectory = cliOpts.BinDirectory
		case "backup-directory":
			opts.Directory = cliOpts.Directory
		case "exclude-dbs":
			opts.ExcludeDbs = cliOpts.ExcludeDbs
		case "include-dbs":
			opts.Dbnames = cliOpts.Dbnames
		case "with-templates":
			opts.WithTemplates = cliOpts.WithTemplates
		case "pause-timeout":
			opts.PauseTimeout = cliOpts.PauseTimeout
		case "jobs":
			opts.Jobs = cliOpts.Jobs
		case "format":
			opts.Format = cliOpts.Format
			for _, dbo := range opts.PerDbOpts {
				dbo.Format = cliOpts.Format
			}
		case "parallel-backup-jobs":
			opts.DirJobs = cliOpts.DirJobs
			for _, dbo := range opts.PerDbOpts {
				dbo.Jobs = cliOpts.DirJobs
			}
		case "checksum-algo":
			opts.SumAlgo = cliOpts.SumAlgo
			for _, dbo := range opts.PerDbOpts {
				dbo.SumAlgo = cliOpts.SumAlgo
			}
		case "purge-older-than":
			opts.PurgeInterval = cliOpts.PurgeInterval
			for _, dbo := range opts.PerDbOpts {
				dbo.PurgeInterval = cliOpts.PurgeInterval
			}
		case "purge-min-keep":
			opts.PurgeKeep = cliOpts.PurgeKeep
			for _, dbo := range opts.PerDbOpts {
				dbo.PurgeKeep = cliOpts.PurgeKeep
			}
		case "pre-backup-hook":
			opts.PreHook = cliOpts.PreHook
		case "post-backup-hook":
			opts.PostHook = cliOpts.PostHook
		case "host":
			opts.Host = cliOpts.Host
		case "port":
			opts.Port = cliOpts.Port
		case "username":
			opts.Username = cliOpts.Username
		case "dbname":
			opts.ConnDb = cliOpts.ConnDb
		}
	}

	return opts
}
