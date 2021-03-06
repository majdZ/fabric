/*
Copyright IBM Corp. 2016 All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

                 http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package configtx

import (
	"fmt"
	"testing"

	"github.com/hyperledger/fabric/common/configtx/api"
	mockconfigtx "github.com/hyperledger/fabric/common/mocks/configtx"
	mockpolicies "github.com/hyperledger/fabric/common/mocks/policies"
	cb "github.com/hyperledger/fabric/protos/common"
	"github.com/hyperledger/fabric/protos/utils"
)

var defaultChain = "DefaultChainID"

func defaultInitializer() *mockconfigtx.Initializer {
	return &mockconfigtx.Initializer{
		Resources: mockconfigtx.Resources{
			PolicyManagerVal: &mockpolicies.Manager{
				Policy: &mockpolicies.Policy{},
			},
		},
		HandlerVal: &mockconfigtx.Handler{},
	}
}

type configPair struct {
	key   string
	value *cb.ConfigValue
}

func makeConfigPair(id, modificationPolicy string, lastModified uint64, data []byte) *configPair {
	return &configPair{
		key: id,
		value: &cb.ConfigValue{
			ModPolicy: modificationPolicy,
			Version:   lastModified,
			Value:     data,
		},
	}
}

func makeConfigEnvelope(chainID string, configPairs ...*configPair) *cb.ConfigEnvelope {
	values := make(map[string]*cb.ConfigValue)
	for _, pair := range configPairs {
		values[pair.key] = pair.value
	}

	return &cb.ConfigEnvelope{
		Config: &cb.Config{
			Header: &cb.ChannelHeader{ChannelId: chainID},
			Channel: &cb.ConfigGroup{
				Values: values,
			},
		},
	}
}

func makeConfigUpdateEnvelope(chainID string, configPairs ...*configPair) *cb.Envelope {
	values := make(map[string]*cb.ConfigValue)
	for _, pair := range configPairs {
		values[pair.key] = pair.value
	}

	config := &cb.ConfigUpdate{
		Header: &cb.ChannelHeader{ChannelId: chainID},
		WriteSet: &cb.ConfigGroup{
			Values: values,
		},
	}
	return &cb.Envelope{
		Payload: utils.MarshalOrPanic(&cb.Payload{
			Header: &cb.Header{
				ChannelHeader: &cb.ChannelHeader{
					Type: int32(cb.HeaderType_CONFIG_UPDATE),
				},
			},
			Data: utils.MarshalOrPanic(&cb.ConfigUpdateEnvelope{
				ConfigUpdate: utils.MarshalOrPanic(config),
			}),
		}),
	}
}

func TestCallback(t *testing.T) {
	var calledBack api.Manager
	callback := func(m api.Manager) {
		calledBack = m
	}

	cm, err := NewManagerImpl(
		makeConfigEnvelope(defaultChain, makeConfigPair("foo", "foo", 0, []byte("foo"))),
		defaultInitializer(), []func(api.Manager){callback})

	if err != nil {
		t.Fatalf("Error constructing config manager: %s", err)
	}

	if calledBack != cm {
		t.Fatalf("Should have called back with the correct manager")
	}
}

// TestDifferentChainID tests that a config update for a different chain ID fails
func TestDifferentChainID(t *testing.T) {
	cm, err := NewManagerImpl(
		makeConfigEnvelope(defaultChain, makeConfigPair("foo", "foo", 0, []byte("foo"))),
		defaultInitializer(), nil)

	if err != nil {
		t.Fatalf("Error constructing config manager: %s", err)
	}

	newConfig := makeConfigUpdateEnvelope("wrongChain", makeConfigPair("foo", "foo", 1, []byte("foo")))

	err = cm.Validate(newConfig)
	if err == nil {
		t.Error("Should have errored when validating a new config set the wrong chain ID")
	}

	err = cm.Apply(newConfig)
	if err == nil {
		t.Error("Should have errored when applying a new config with the wrong chain ID")
	}
}

// TestOldConfigReplay tests that resubmitting a config for a sequence number which is not newer is ignored
func TestOldConfigReplay(t *testing.T) {
	cm, err := NewManagerImpl(
		makeConfigEnvelope(defaultChain, makeConfigPair("foo", "foo", 0, []byte("foo"))),
		defaultInitializer(), nil)

	if err != nil {
		t.Fatalf("Error constructing config manager: %s", err)
	}

	newConfig := makeConfigUpdateEnvelope(defaultChain, makeConfigPair("foo", "foo", 0, []byte("foo")))

	err = cm.Validate(newConfig)
	if err == nil {
		t.Error("Should have errored when validating a config that is not a newer sequence number")
	}

	err = cm.Apply(newConfig)
	if err == nil {
		t.Error("Should have errored when applying a config that is not a newer sequence number")
	}
}

// TestValidConfigChange tests the happy path of updating a config value with no defaultModificationPolicy
func TestValidConfigChange(t *testing.T) {
	cm, err := NewManagerImpl(
		makeConfigEnvelope(defaultChain, makeConfigPair("foo", "foo", 0, []byte("foo"))),
		defaultInitializer(), nil)

	if err != nil {
		t.Fatalf("Error constructing config manager: %s", err)
	}

	newConfig := makeConfigUpdateEnvelope(defaultChain, makeConfigPair("foo", "foo", 1, []byte("foo")))

	err = cm.Validate(newConfig)
	if err != nil {
		t.Errorf("Should not have errored validating config: %s", err)
	}

	err = cm.Apply(newConfig)
	if err != nil {
		t.Errorf("Should not have errored applying config: %s", err)
	}
}

// TestConfigChangeRegressedSequence tests to make sure that a new config cannot roll back one of the
// config values while advancing another
func TestConfigChangeRegressedSequence(t *testing.T) {
	cm, err := NewManagerImpl(
		makeConfigEnvelope(defaultChain, makeConfigPair("foo", "foo", 1, []byte("foo"))),
		defaultInitializer(), nil)

	if err != nil {
		t.Fatalf("Error constructing config manager: %s", err)
	}

	newConfig := makeConfigUpdateEnvelope(
		defaultChain,
		makeConfigPair("foo", "foo", 0, []byte("foo")),
		makeConfigPair("bar", "bar", 2, []byte("bar")),
	)

	err = cm.Validate(newConfig)
	if err == nil {
		t.Error("Should have errored validating config because foo's sequence number regressed")
	}

	err = cm.Apply(newConfig)
	if err == nil {
		t.Error("Should have errored applying config because foo's sequence number regressed")
	}
}

// TestConfigChangeOldSequence tests to make sure that a new config cannot roll back one of the
// config values while advancing another
func TestConfigChangeOldSequence(t *testing.T) {
	cm, err := NewManagerImpl(
		makeConfigEnvelope(defaultChain, makeConfigPair("foo", "foo", 1, []byte("foo"))),
		defaultInitializer(), nil)

	if err != nil {
		t.Fatalf("Error constructing config manager: %s", err)
	}

	newConfig := makeConfigUpdateEnvelope(
		defaultChain,
		makeConfigPair("foo", "foo", 2, []byte("foo")),
		makeConfigPair("bar", "bar", 1, []byte("bar")),
	)

	err = cm.Validate(newConfig)
	if err == nil {
		t.Error("Should have errored validating config because bar was new but its sequence number was old")
	}

	err = cm.Apply(newConfig)
	if err == nil {
		t.Error("Should have errored applying config because bar was new but its sequence number was old")
	}
}

// TestConfigImplicitDelete tests to make sure that a new config does not implicitly delete config items
// by omitting them in the new config
func TestConfigImplicitDelete(t *testing.T) {
	cm, err := NewManagerImpl(
		makeConfigEnvelope(
			defaultChain,
			makeConfigPair("foo", "foo", 0, []byte("foo")),
			makeConfigPair("bar", "bar", 0, []byte("bar")),
		),
		defaultInitializer(), nil)

	if err != nil {
		t.Fatalf("Error constructing config manager: %s", err)
	}

	newConfig := makeConfigUpdateEnvelope(
		defaultChain,
		makeConfigPair("bar", "bar", 1, []byte("bar")),
	)

	err = cm.Validate(newConfig)
	if err == nil {
		t.Error("Should have errored validating config because foo was implicitly deleted")
	}

	err = cm.Apply(newConfig)
	if err == nil {
		t.Error("Should have errored applying config because foo was implicitly deleted")
	}
}

// TestEmptyConfigUpdate tests to make sure that an empty config is rejected as an update
func TestEmptyConfigUpdate(t *testing.T) {
	cm, err := NewManagerImpl(
		makeConfigEnvelope(defaultChain, makeConfigPair("foo", "foo", 0, []byte("foo"))),
		defaultInitializer(), nil)

	if err != nil {
		t.Fatalf("Error constructing config manager: %s", err)
	}

	newConfig := &cb.Envelope{}

	err = cm.Validate(newConfig)
	if err == nil {
		t.Error("Should not errored validating config because new config is empty")
	}

	err = cm.Apply(newConfig)
	if err == nil {
		t.Error("Should not errored applying config because new config is empty")
	}
}

// TestSilentConfigModification tests to make sure that even if a validly signed new config for an existing sequence number
// is substituted into an otherwise valid new config, that the new config is rejected for attempting a modification without
// increasing the config item's LastModified
func TestSilentConfigModification(t *testing.T) {
	cm, err := NewManagerImpl(
		makeConfigEnvelope(
			defaultChain,
			makeConfigPair("foo", "foo", 0, []byte("foo")),
			makeConfigPair("bar", "bar", 0, []byte("bar")),
		),
		defaultInitializer(), nil)

	if err != nil {
		t.Fatalf("Error constructing config manager: %s", err)
	}

	newConfig := makeConfigUpdateEnvelope(
		defaultChain,
		makeConfigPair("foo", "foo", 0, []byte("different")),
		makeConfigPair("bar", "bar", 1, []byte("bar")),
	)

	err = cm.Validate(newConfig)
	if err == nil {
		t.Error("Should not errored validating config because foo was silently modified (despite modification allowed by policy)")
	}

	err = cm.Apply(newConfig)
	if err == nil {
		t.Error("Should not errored applying config because foo was silently modified (despite modification allowed by policy)")
	}
}

// TestConfigChangeViolatesPolicy checks to make sure that if policy rejects the validation of a config item that
// it is rejected in a config update
func TestConfigChangeViolatesPolicy(t *testing.T) {
	initializer := defaultInitializer()
	cm, err := NewManagerImpl(
		makeConfigEnvelope(defaultChain, makeConfigPair("foo", "foo", 0, []byte("foo"))),
		initializer, nil)

	if err != nil {
		t.Fatalf("Error constructing config manager: %s", err)
	}
	// Set the mock policy to error
	initializer.Resources.PolicyManagerVal.Policy.Err = fmt.Errorf("err")

	newConfig := makeConfigUpdateEnvelope(defaultChain, makeConfigPair("foo", "foo", 1, []byte("foo")))

	err = cm.Validate(newConfig)
	if err == nil {
		t.Error("Should have errored validating config because policy rejected modification")
	}

	err = cm.Apply(newConfig)
	if err == nil {
		t.Error("Should have errored applying config because policy rejected modification")
	}
}

// TestUnchangedConfigViolatesPolicy checks to make sure that existing config items are not revalidated against their modification policies
// as the policy may have changed, certs revoked, etc. since the config was adopted.
func TestUnchangedConfigViolatesPolicy(t *testing.T) {
	initializer := defaultInitializer()
	cm, err := NewManagerImpl(
		makeConfigEnvelope(defaultChain, makeConfigPair("foo", "foo", 0, []byte("foo"))),
		initializer, nil)

	if err != nil {
		t.Fatalf("Error constructing config manager: %s", err)
	}

	// Set the mock policy to error
	initializer.Resources.PolicyManagerVal.PolicyMap = make(map[string]*mockpolicies.Policy)
	initializer.Resources.PolicyManagerVal.PolicyMap["foo"] = &mockpolicies.Policy{Err: fmt.Errorf("err")}

	newConfig := makeConfigUpdateEnvelope(
		defaultChain,
		makeConfigPair("foo", "foo", 0, []byte("foo")),
		makeConfigPair("bar", "bar", 1, []byte("foo")),
	)

	err = cm.Validate(newConfig)
	if err != nil {
		t.Errorf("Should not have errored validating config, but got %s", err)
	}

	err = cm.Apply(newConfig)
	if err != nil {
		t.Errorf("Should not have errored applying config, but got %s", err)
	}
}

// TestInvalidProposal checks that even if the policy allows the transaction and the sequence etc. is well formed,
// that if the handler does not accept the config, it is rejected
func TestInvalidProposal(t *testing.T) {
	initializer := defaultInitializer()
	cm, err := NewManagerImpl(
		makeConfigEnvelope(defaultChain, makeConfigPair("foo", "foo", 0, []byte("foo"))),
		initializer, nil)

	if err != nil {
		t.Fatalf("Error constructing config manager: %s", err)
	}

	initializer.HandlerVal = &mockconfigtx.Handler{ErrorForProposeConfig: fmt.Errorf("err")}

	newConfig := makeConfigUpdateEnvelope(defaultChain, makeConfigPair("foo", "foo", 1, []byte("foo")))

	err = cm.Validate(newConfig)
	if err == nil {
		t.Error("Should have errored validating config because the handler rejected it")
	}

	err = cm.Apply(newConfig)
	if err == nil {
		t.Error("Should have errored applying config because the handler rejected it")
	}
}

// TestMissingHeader checks that a config envelope with a missing header causes the config to be rejected
func TestMissingHeader(t *testing.T) {
	group := cb.NewConfigGroup()
	group.Values["foo"] = &cb.ConfigValue{}
	_, err := NewManagerImpl(
		&cb.ConfigEnvelope{Config: &cb.Config{Channel: group}},
		defaultInitializer(), nil)

	if err == nil {
		t.Error("Should have errored creating the config manager because of the missing header")
	}
}

// TestMissingChainID checks that a config item with a missing chainID causes the config to be rejected
func TestMissingChainID(t *testing.T) {
	_, err := NewManagerImpl(
		makeConfigEnvelope("", makeConfigPair("foo", "foo", 0, []byte("foo"))),
		defaultInitializer(), nil)

	if err == nil {
		t.Error("Should have errored creating the config manager because of the missing header")
	}
}
