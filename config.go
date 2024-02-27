package main

import (
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		Port string `yaml:"port"`
	} `yaml:"server"`
	Rest struct {
		Host  string `yaml:"host"`
		Port  string `yaml:"port"`
		Rest  string `yaml:"restPath"`
		Nodes struct {
			GetQuestions    string `yaml:"getQuestions"`
			SaveTestResults string `yaml:"saveTestResult"`
		} `yaml:"nodes"`
	} `yaml:"rest"`
}

func readConf(cfg *Config) {
	f, err := os.Open("config.yaml")
	if err != nil {
		processError(err)
	}
	defer f.Close()

	decoder := yaml.NewDecoder(f)
	err = decoder.Decode(cfg)
	if err != nil {
		processError(err)
	}
}

func getQuestionUrl(cfg Config) string {
	return strings.Join([]string{
		"http://",
		cfg.Rest.Host,
		":",
		cfg.Rest.Port,
		cfg.Rest.Rest,
		cfg.Rest.Nodes.GetQuestions,
	}, "")
}

func getSaveUrl(cfg Config) string {
	return strings.Join([]string{
		"http://",
		cfg.Rest.Host,
		":",
		cfg.Rest.Port,
		cfg.Rest.Rest,
		cfg.Rest.Nodes.SaveTestResults,
	}, "")
}
