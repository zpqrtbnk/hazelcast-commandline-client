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
    iserialization "github.com/hazelcast/hazelcast-go-client"
    proto "github.com/hazelcast/hazelcast-go-client"
)


const(
    ListRemoveCodecRequestMessageType  = int32(0x050500)
    ListRemoveCodecResponseMessageType = int32(0x050501)

    ListRemoveCodecRequestInitialFrameSize = proto.PartitionIDOffset + proto.IntSizeInBytes

    ListRemoveResponseResponseOffset = proto.ResponseBackupAcksOffset + proto.ByteSizeInBytes
)

// Removes the first occurrence of the specified element from this list, if it is present (optional operation).
// If this list does not contain the element, it is unchanged.
// Returns true if this list contained the specified element (or equivalently, if this list changed as a result of the call).

func EncodeListRemoveRequest(name string, value iserialization.Data) *proto.ClientMessage {
    clientMessage := proto.NewClientMessageForEncode()
    clientMessage.SetRetryable(false)

    initialFrame := proto.NewFrameWith(make([]byte, ListRemoveCodecRequestInitialFrameSize), proto.UnfragmentedMessage)
    clientMessage.AddFrame(initialFrame)
    clientMessage.SetMessageType(ListRemoveCodecRequestMessageType)
    clientMessage.SetPartitionId(-1)

    EncodeString(clientMessage, name)
    EncodeData(clientMessage, value)

    return clientMessage
}

func DecodeListRemoveResponse(clientMessage *proto.ClientMessage) bool {
    frameIterator := clientMessage.FrameIterator()
    initialFrame := frameIterator.Next()

    return DecodeBoolean(initialFrame.Content, ListRemoveResponseResponseOffset)
}
