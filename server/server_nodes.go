package server

import (
	"time"

	"github.com/gopcua/opcua/id"
	"github.com/gopcua/opcua/server/attrs"
	"github.com/gopcua/opcua/ua"
)

func namespaceArrayValueFunc(s *Server) ValueFunc {
	return func() *ua.DataValue {
		names := s.Namespaces()
		ns := make([]string, len(names))
		for i := range names {
			ns[i] = names[i].Name()
		}
		return DataValueFromValue(ns)
	}
}

// patchNodeValueFunc sets a live Value provider on an imported NodeSet2 variable
// without replacing the node (which would drop mandatory metadata attributes).
func patchNodeValueFunc(ns *NodeNameSpace, nodeID uint32, vf ValueFunc) {
	if n := ns.Node(ua.NewNumericNodeID(0, nodeID)); n != nil {
		n.SetValueFunc(vf)
	}
}

// patchRuntimeServerNodes wires live Value providers onto standard Server
// variables already imported from NodeSet2. Calling AddNode with a sparse
// replacement drops mandatory attributes (Historizing, ArrayDimensions, etc.)
// and strict clients flag BadAttributeIdInvalid.
func patchRuntimeServerNodes(s *Server, ns *NodeNameSpace) {
	if n := ns.Node(ua.NewNumericNodeID(0, id.Server_NamespaceArray)); n != nil {
		n.SetValueFunc(namespaceArrayValueFunc(s))
	} else {
		ns.AddNode(NamespacesNode(s))
	}

	status := s.Status()
	startTime := status.StartTime

	patchNodeValueFunc(ns, id.Server_ServerStatus, func() *ua.DataValue {
		return DataValueFromValue(ua.NewExtensionObject(s.Status()))
	})
	patchNodeValueFunc(ns, id.Server_ServerStatus_State, func() *ua.DataValue {
		return DataValueFromValue(int32(s.Status().State))
	})
	patchNodeValueFunc(ns, id.Server_ServerStatus_BuildInfo_ManufacturerName, func() *ua.DataValue {
		return DataValueFromValue(s.cfg.manufacturerName)
	})
	patchNodeValueFunc(ns, id.Server_ServerStatus_BuildInfo_ProductName, func() *ua.DataValue {
		return DataValueFromValue(s.cfg.productName)
	})
	patchNodeValueFunc(ns, id.Server_ServerStatus_BuildInfo_ProductURI, func() *ua.DataValue {
		return DataValueFromValue(s.cfg.applicationURI)
	})
	patchNodeValueFunc(ns, id.Server_ServerStatus_BuildInfo_SoftwareVersion, func() *ua.DataValue {
		return DataValueFromValue(s.cfg.softwareVersion)
	})
	patchNodeValueFunc(ns, id.Server_ServerStatus_BuildInfo_BuildNumber, func() *ua.DataValue {
		return DataValueFromValue(s.cfg.softwareVersion)
	})
	patchNodeValueFunc(ns, id.Server_ServerStatus_BuildInfo_BuildDate, func() *ua.DataValue {
		return DataValueFromValue(startTime)
	})
	patchNodeValueFunc(ns, id.Server_ServerStatus_StartTime, func() *ua.DataValue {
		return DataValueFromValue(startTime)
	})
	patchNodeValueFunc(ns, id.Server_ServerStatus_CurrentTime, func() *ua.DataValue {
		return DataValueFromValue(time.Now())
	})
	patchNodeValueFunc(ns, id.Server_ServerStatus_SecondsTillShutdown, func() *ua.DataValue {
		return DataValueFromValue(uint32(0))
	})
	patchNodeValueFunc(ns, id.Server_ServerStatus_ShutdownReason, func() *ua.DataValue {
		return DataValueFromValue(&ua.LocalizedText{})
	})

	patchNodeValueFunc(ns, id.Server_ServerCapabilities_OperationLimits_MaxNodesPerRead, func() *ua.DataValue {
		return DataValueFromValue(s.cfg.cap.OperationalLimits.MaxNodesPerRead)
	})
}

func NamespacesNode(s *Server) *Node {
	propertyTypeID := ua.NewNumericExpandedNodeID(0, id.PropertyType)
	access := byte(ua.AccessLevelTypeCurrentRead)
	return NewNode(
		ua.NewNumericNodeID(0, id.Server_NamespaceArray),
		map[ua.AttributeID]*ua.DataValue{
			ua.AttributeIDBrowseName:              DataValueFromValue(attrs.BrowseName("NamespaceArray")),
			ua.AttributeIDDisplayName:             DataValueFromValue(attrs.DisplayName("NamespaceArray", "")),
			ua.AttributeIDNodeClass:               DataValueFromValue(uint32(ua.NodeClassVariable)),
			ua.AttributeIDDataType:                DataValueFromValue(ua.NewNumericNodeID(0, id.String)),
			ua.AttributeIDValueRank:               DataValueFromValue(int32(1)),
			ua.AttributeIDArrayDimensions:         DataValueFromValue([]uint32{0}),
			ua.AttributeIDAccessLevel:             DataValueFromValue(access),
			ua.AttributeIDUserAccessLevel:         DataValueFromValue(access),
			ua.AttributeIDAccessLevelEx:           DataValueFromValue(uint32(access)),
			ua.AttributeIDMinimumSamplingInterval: DataValueFromValue(float64(1000)),
			ua.AttributeIDHistorizing:             DataValueFromValue(false),
		},
		[]*ua.ReferenceDescription{
			{
				ReferenceTypeID: ua.NewNumericNodeID(0, id.HasTypeDefinition),
				IsForward:       true,
				NodeID:          propertyTypeID,
				BrowseName:      attrs.BrowseName("PropertyType"),
				DisplayName:     attrs.DisplayName("PropertyType", ""),
				NodeClass:       ua.NodeClassVariableType,
				TypeDefinition:  propertyTypeID,
			},
		},
		namespaceArrayValueFunc(s),
	)
}

func RootNode() *Node {
	return NewNode(
		ua.NewNumericNodeID(0, id.RootFolder),
		map[ua.AttributeID]*ua.DataValue{
			ua.AttributeIDNodeClass:  DataValueFromValue(attrs.NodeClass(ua.NodeClassObject)),
			ua.AttributeIDBrowseName: DataValueFromValue(attrs.BrowseName("Root")),
			ua.AttributeIDDataType:   DataValueFromValue(ua.NewNumericExpandedNodeID(0, id.DataTypesFolder)),
		},
		nil,
		nil,
	)
}
