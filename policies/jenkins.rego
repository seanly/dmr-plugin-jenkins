# OPA Policy for Jenkins plugin
# Rego V1 format

package dmr

# Allow: Read-only operations
decision := {"action": "allow", "reason": "jenkins read-only query", "risk": "low"} if {
	input.tool in [
		"jenkinsInstances",
		"jenkinsGetJob",
		"jenkinsSearchJobs",
		"jenkinsListBuilds",
		"jenkinsGetBuild",
		"jenkinsGetConsoleText"
	]
}

# Require approval: Write operations
jenkins_write_tools contains "jenkinsTriggerBuild"

jenkins_write_tools contains "jenkinsCreateJob"
jenkins_write_tools contains "jenkinsCreatePipelineJob"
jenkins_write_tools contains "jenkinsCloneJob"
jenkins_write_tools contains "jenkinsUpdateJobConfig"
jenkins_write_tools contains "jenkinsDeleteJob"

decision := {"action": "require_approval", "reason": msg, "risk": "medium"} if {
	input.tool in jenkins_write_tools
	msg := sprintf("jenkins write: %s", [input.tool])
}
