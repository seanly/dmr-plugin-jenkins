package main

import "testing"

func TestValidateConfig(t *testing.T) {
	verify := true
	ok := &JenkinsPluginConfig{
		DefaultInstance: "a",
		Instances: []JenkinsInstanceConfig{
			{ID: "a", BaseURL: "https://j.example/", Username: "u", APIToken: "t", VerifyTLS: &verify},
		},
	}
	if err := validateConfig(ok); err != nil {
		t.Fatal(err)
	}

	bad := &JenkinsPluginConfig{
		DefaultInstance: "missing",
		Instances:       ok.Instances,
	}
	if err := validateConfig(bad); err == nil {
		t.Fatal("expected error")
	}
}
