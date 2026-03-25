package dmr

import future.keywords.if
import future.keywords.in
import future.keywords.contains

# Jenkins plugin: all mutating tools require human approval by default.
# Read tools (jenkinsInstances, jenkinsGetJob, jenkinsListBuilds, jenkinsGetBuild, jenkinsGetConsoleText)
# are not listed here and fall through to default policy.

jenkins_write_tools contains "jenkinsTriggerBuild"

jenkins_write_tools contains "jenkinsCreateJob"

jenkins_write_tools contains "jenkinsCreatePipelineJob"

jenkins_write_tools contains "jenkinsCloneJob"

# jenkins_write_tools contains "jenkinsUpdateJobConfig"
# jenkins_write_tools contains "jenkinsDeleteJob"

decision = {"action": "require_approval", "reason": msg, "risk": "medium"} if {
	input.tool in jenkins_write_tools
	msg := sprintf("jenkins write: %s", [input.tool])
}
