package deployer

import (
	"testing"

	"github.com/coinbase/odin/aws/mocks"
	"github.com/coinbase/odin/deployer/models"
	"github.com/coinbase/step/machine"
	"github.com/coinbase/step/utils/to"
	"github.com/stretchr/testify/assert"
)

///////////////
// Successful Tests
///////////////

func Test_Successful_Execution_Works(t *testing.T) {
	// Should end in Alert Bad Thing Happened State
	release := models.MockRelease(t)
	assertSuccessfulExecution(t, release)
}

func Test_Successful_Execution_Works_With_Minimal_Release(t *testing.T) {
	// Should end in Alert Bad Thing Happened State
	release := models.MockMinimalRelease(t)
	assertSuccessfulExecution(t, release)
}

///////////////
// Unsuccessful Tests
///////////////

func Test_UnsuccessfulDeploy_Bad_Userdata_SHA(t *testing.T) {
	// Should end in Alert Bad Thing Happened State
	release := models.MockMinimalRelease(t)

	// Should end in Alert Bad Thing Happened State
	stateMachine := createTestStateMachine(t, models.MockAwsClients(release))
	release.UserDataSHA256 = to.Strp("asfhjoias")

	output, err := stateMachine.ExecuteToMap(release)

	assert.Error(t, err)
	assert.Equal(t, "FailureClean", output["Error"])

	assert.Equal(t, []string{

		"Validate",
		machine.TaskFnName("Validate"),

		"FailureClean",
	}, stateMachine.ExecutionPath())
}

func Test_UnsuccessfulDeploy_Execution_Works(t *testing.T) {
	release := models.MockRelease(t)
	release.Timeout = to.Intp(-10) // This will cause immediate timeout

	// Should end in Alert Bad Thing Happened State
	stateMachine := createTestStateMachine(t, models.MockAwsClients(release))

	output, err := stateMachine.ExecuteToMap(release)

	assert.Error(t, err)
	assert.Equal(t, "FailureClean", output["Error"])

	assert.Equal(t, []string{

		"Validate",
		machine.TaskFnName("Validate"),

		"Lock",
		machine.TaskFnName("Lock"),

		"ValidateResources",
		machine.TaskFnName("ValidateResources"),

		"Deploy",
		machine.TaskFnName("Deploy"),

		"ReleaseLockFailure",
		machine.TaskFnName("ReleaseLockFailure"),
		"FailureClean",
	}, stateMachine.ExecutionPath())
}

///////////////
// MACHINE FetchDeploy INTERGATION TESTS
///////////////

func Test_Execution_FetchDeploy_BadInputError(t *testing.T) {
	// Should end in clean state as nothing has happened yet
	stateMachine := createTestStateMachine(t, models.MockAwsClients(models.MockRelease(t)))

	output, err := stateMachine.ExecuteToMap(struct{}{})

	assert.Error(t, err)
	assert.Equal(t, "FailureClean", output["Error"])

	assert.Equal(t, stateMachine.ExecutionPath(), []string{

		"Validate",
		machine.TaskFnName("Validate"),
		"FailureClean",
	})
}

func Test_Execution_FetchDeploy_UnkownKeyInput(t *testing.T) {
	// Should end in clean state as nothing has happened yet
	stateMachine := createTestStateMachine(t, models.MockAwsClients(models.MockRelease(t)))

	output, err := stateMachine.ExecuteToMap(struct{ Unkown string }{Unkown: "asd"})

	assert.Error(t, err)
	assert.Equal(t, "FailureClean", output["Error"])
	assert.Regexp(t, "unknown field", stateMachine.LastOutput())

	assert.Equal(t, stateMachine.ExecutionPath(), []string{

		"Validate",
		machine.TaskFnName("Validate"),
		"FailureClean",
	})
}

func Test_Execution_FetchDeploy_BadInputError_Unamarshalling(t *testing.T) {
	// Should end in clean state as nothing has happened yet
	stateMachine := createTestStateMachine(t, models.MockAwsClients(models.MockRelease(t)))

	output, err := stateMachine.ExecuteToMap(struct{ Subnets string }{Subnets: ""})

	assert.Error(t, err)
	assert.Equal(t, "FailureClean", output["Error"])

	assert.Equal(t, stateMachine.ExecutionPath(), []string{

		"Validate",
		machine.TaskFnName("Validate"),
		"FailureClean",
	})
}

func Test_Execution_FetchDeploy_LockError(t *testing.T) {
	release := models.MockRelease(t)

	// Should retry a few times, then end in clean state as nothing was created
	awsClients := models.MockAwsClients(release)

	// Force a lock error by making it look like it was already aquired
	awsClients.S3.AddGetObject(*release.LockPath(), `{"uuid": "already"}`, nil)

	stateMachine := createTestStateMachine(t, awsClients)

	output, err := stateMachine.ExecuteToMap(release)

	assert.Error(t, err)
	assert.Equal(t, "FailureClean", output["Error"])

	assert.Equal(t, stateMachine.ExecutionPath(), []string{

		"Validate",
		machine.TaskFnName("Validate"),

		"Lock",
		machine.TaskFnName("Lock"),
		"FailureClean",
	})
}

func Test_Execution_CheckHealthy_HaltError_WithTermination(t *testing.T) {
	// Should end in Alert Bad Thing Happened State
	release := models.MockRelease(t)
	maws := models.MockAwsClients(release)
	maws.ASG.DescribeAutoScalingGroupsPageResp = nil

	termingASG := mocks.MakeMockASG("odin", *release.ProjectName, *release.ConfigName, "web", "Old release")
	termingASG.Instances[0].LifecycleState = to.Strp("Terminating")

	maws.ASG.AddASG(termingASG)

	stateMachine := createTestStateMachine(t, maws)

	_, err := stateMachine.ExecuteToMap(release)

	assert.Error(t, err)
	assert.Regexp(t, "HaltError", stateMachine.LastOutput())
	assert.Regexp(t, "success\":false", stateMachine.LastOutput())

	assert.Equal(t, stateMachine.ExecutionPath(), []string{

		"Validate",
		machine.TaskFnName("Validate"),

		"Lock",
		machine.TaskFnName("Lock"),

		"ValidateResources",
		machine.TaskFnName("ValidateResources"),

		"Deploy",
		machine.TaskFnName("Deploy"),
		"WaitForDeploy",
		"WaitForHealthy",

		"CheckHealthy",
		machine.TaskFnName("CheckHealthy"),

		"CleanUpFailure",
		machine.TaskFnName("CleanUpFailure"),

		"ReleaseLockFailure",
		machine.TaskFnName("ReleaseLockFailure"),
		"FailureClean",
	})
}

func Test_Execution_CheckHealthy_Never_Healthy_ELB(t *testing.T) {
	// Should end in Alert Bad Thing Happened State
	release := models.MockRelease(t)

	maws := models.MockAwsClients(release)
	maws.ELB.DescribeInstanceHealthResp["web-elb"] = &mocks.DescribeInstanceHealthResponse{}

	stateMachine := createTestStateMachine(t, maws)

	_, err := stateMachine.ExecuteToMap(release)

	assert.Error(t, err)

	ep := stateMachine.ExecutionPath()
	assert.Equal(t, []string{

		"Validate",
		machine.TaskFnName("Validate"),

		"Lock",
		machine.TaskFnName("Lock"),

		"ValidateResources",
		machine.TaskFnName("ValidateResources"),

		"Deploy",
		machine.TaskFnName("Deploy"),
		"WaitForDeploy",
		"WaitForHealthy",
		"CheckHealthy",
		machine.TaskFnName("CheckHealthy"),
	}, ep[0:12])

	assert.Equal(t, []string{

		"CleanUpFailure",
		machine.TaskFnName("CleanUpFailure"),

		"ReleaseLockFailure",
		machine.TaskFnName("ReleaseLockFailure"),
		"FailureClean",
	}, ep[len(ep)-5:len(ep)])

	assert.Regexp(t, "Timeout", stateMachine.LastOutput())
	assert.Regexp(t, "success\":false", stateMachine.LastOutput())
}

func Test_Execution_CheckHealthy_Never_Healthy_TG(t *testing.T) {
	// Should end in Alert Bad Thing Happened State
	release := models.MockRelease(t)

	maws := models.MockAwsClients(release)
	maws.ALB.DescribeTargetHealthResp["web-elb-target"] = &mocks.DescribeTargetHealthResponse{}

	stateMachine := createTestStateMachine(t, maws)

	_, err := stateMachine.ExecuteToMap(release)

	assert.Error(t, err)

	ep := stateMachine.ExecutionPath()
	assert.Equal(t, []string{

		"Validate",
		machine.TaskFnName("Validate"),

		"Lock",
		machine.TaskFnName("Lock"),

		"ValidateResources",
		machine.TaskFnName("ValidateResources"),

		"Deploy",
		machine.TaskFnName("Deploy"),
		"WaitForDeploy",
		"WaitForHealthy",

		"CheckHealthy",
		machine.TaskFnName("CheckHealthy"),
	}, ep[0:12])

	assert.Equal(t, []string{

		"CleanUpFailure",
		machine.TaskFnName("CleanUpFailure"),

		"ReleaseLockFailure",
		machine.TaskFnName("ReleaseLockFailure"),
		"FailureClean",
	}, ep[len(ep)-5:len(ep)])

	assert.Regexp(t, "Timeout", stateMachine.LastOutput())
	assert.Regexp(t, "success\":false", stateMachine.LastOutput())
}
