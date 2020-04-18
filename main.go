package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"time"

	"github.com/electrotumbao/amqp-cgi-bridge/bridge"
	"github.com/skolodyazhnyy/go-common/log"
	"gopkg.in/yaml.v2"
)

var version = "unknown"
var commit = "unknown"

var config struct {
	AMQPURL string `yaml:"amqp_url"`
	Env     map[string]string
	FastCGI struct {
		Net        string
		Addr       string
		ScriptName string `yaml:"script_name"`
	}
	Consumers []struct {
		Queue          string
		MessageTTL     int   `yaml:"message_ttl"`
		Prefetch       *int
		Parallelism    int
		FailureTimeout time.Duration
		Env            map[string]string
		FastCGI        struct {
			Net        string
			Addr       string
			ScriptName string `yaml:"script_name"`
		}
	}
}

func load(filename string, v interface{}) error {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}

	return yaml.Unmarshal(data, v)
}

func main() {
	// parse flags
	filename := flag.String("config", "config.yml", "Configuration")
	logfmt := flag.String("log", "text", "Log format: json or text")
	printVersion := flag.Bool("v", false, "Print version")
	flag.Parse()

	if *printVersion {
		fmt.Println("Version", version)
		fmt.Println("Commit", commit)
		os.Exit(0)
	}

	logger := log.New(*logfmt, os.Stdout, log.DefaultTextFormat).With(log.R{
		"app":     "amqp-cgi-bridge",
		"version": version,
	})

	if err := load(*filename, &config); err != nil {
		logger.Fatal(err)
	}

	ctx := context.Background()
	var queues []bridge.Queue

	if config.FastCGI.Net == "" {
		config.FastCGI.Net = "tcp"
	}

	if config.FastCGI.Addr == "" {
		config.FastCGI.Addr = "127.0.0.1:9000"
	}

	if config.FastCGI.ScriptName == "" {
		config.FastCGI.ScriptName = "index.php"
	}

	for _, c := range config.Consumers {
		if c.FastCGI.Net == "" {
			c.FastCGI.Net = config.FastCGI.Net
		}

		if c.FastCGI.Addr == "" {
			c.FastCGI.Addr = config.FastCGI.Addr
		}

		if c.FastCGI.ScriptName == "" {
			c.FastCGI.ScriptName = config.FastCGI.ScriptName
		}

		p := bridge.NewFastCGIProcessor(
			c.FastCGI.Net,
			c.FastCGI.Addr,
			c.FastCGI.ScriptName,
			logger.Channel("fastcgi").With(log.R{
				"script_name": c.FastCGI.ScriptName,
			}),
		)

		env := config.Env
		if env == nil {
			env = map[string]string{}
		}
		if c.Env != nil {
			for k, v := range c.Env {
				env[k] = v
			}
		}
		if len(env) > 0 {
			p = bridge.ProcessorWithEnv(p, env)
		}

		if c.Parallelism <= 0 {
			c.Parallelism = 1
		}

		if c.Prefetch == nil {
			c.Prefetch = &c.Parallelism
		}

		if c.FailureTimeout == 0 {
			c.FailureTimeout = 10 * time.Second
		}

		queues = append(queues, bridge.Queue{
			Name:           c.Queue,
			Prefetch:       *c.Prefetch,
			Parallelism:    c.Parallelism,
			MessageTTL:     c.MessageTTL,
			FailureTimeout: c.FailureTimeout,
			Processor:      p,
		})
	}

	cons := bridge.NewAMQPConsumer(ctx, config.AMQPURL, queues, logger.Channel("amqp"))

	signals := make(chan os.Signal)
	signal.Notify(signals, os.Interrupt, os.Kill)

	s := <-signals
	logger.Infof("Signal %v received, stopping...", s)

	cons.Stop()
}
