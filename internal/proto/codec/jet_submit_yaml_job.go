/*
* Copyright (c) 2008-2023, Hazelcast, Inc. All Rights Reserved.
*
* Licensed under the Apache License, Version 2.0 (the "License")
* you may not use this file except in compliance with the License.
* You may obtain a copy of the License at
*
* http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
 */

package codec

import (
	proto "github.com/hazelcast/hazelcast-go-client"
)

const (
	JetSubmitYamlJobCodecRequestMessageType  = int32(0xFE0102)
	JetSubmitYamlJobCodecResponseMessageType = int32(0xFE0103)

	JetSubmitYamlJobCodecRequestJobIdOffset      = proto.PartitionIDOffset + proto.IntSizeInBytes
	JetSubmitYamlJobCodecRequestDryRunOffset     = JetSubmitYamlJobCodecRequestJobIdOffset + proto.LongSizeInBytes
	JetSubmitYamlJobCodecRequestInitialFrameSize = JetSubmitYamlJobCodecRequestDryRunOffset + proto.BooleanSizeInBytes
)

func EncodeJetSubmitYamlJobRequest(jobId int64, dryRun bool, yaml string) *proto.ClientMessage {
	clientMessage := proto.NewClientMessageForEncode()
	clientMessage.SetRetryable(true)

	initialFrame := proto.NewFrameWith(make([]byte, JetSubmitYamlJobCodecRequestInitialFrameSize), proto.UnfragmentedMessage)
	EncodeLong(initialFrame.Content, JetSubmitYamlJobCodecRequestJobIdOffset, jobId)
	EncodeBoolean(initialFrame.Content, JetSubmitYamlJobCodecRequestDryRunOffset, dryRun)
	clientMessage.AddFrame(initialFrame)
	clientMessage.SetMessageType(JetSubmitYamlJobCodecRequestMessageType)
	clientMessage.SetPartitionId(-1)

	EncodeString(clientMessage, yaml)

	return clientMessage
}
