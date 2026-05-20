package server

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/gopcua/opcua/id"
	"github.com/gopcua/opcua/schema"
	"github.com/gopcua/opcua/ua"
)

// nodesetAliases builds a map alias->NodeID string from the NodeSet aliases
// table. It returns an empty map if the table is missing.
func nodesetAliases(nodes *schema.UANodeSet) map[string]string {
	out := make(map[string]string)
	if nodes.Aliases == nil {
		return out
	}
	for _, a := range nodes.Aliases.Alias {
		if a == nil {
			continue
		}
		out[a.AliasAttr] = a.Value
	}
	return out
}

// nodesetResolveDataType resolves a DataType reference from the NodeSet2 XML
// (either a NodeId string like "i=12" or an alias name like "String") to a
// concrete NodeID. Falls back to BaseDataType (i=24) when the value is empty
// or cannot be resolved.
func nodesetResolveDataType(value string, aliases map[string]string) *ua.NodeID {
	if value == "" {
		return ua.NewNumericNodeID(0, id.BaseDataType)
	}
	if real, ok := aliases[value]; ok && real != "" {
		value = real
	}
	nid, err := ua.ParseNodeID(value)
	if err != nil {
		return ua.NewNumericNodeID(0, id.BaseDataType)
	}
	return nid
}

// nodesetParseArrayDimensions parses an "ArrayDimensions" XML attribute
// (comma-separated unsigned integers like "0,0,0") into a slice of uint32.
// Returns nil when empty or invalid so the caller can skip the attribute.
func nodesetParseArrayDimensions(value string) []uint32 {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	dims := make([]uint32, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.ParseUint(p, 10, 32)
		if err != nil {
			return nil
		}
		dims = append(dims, uint32(v))
	}
	if len(dims) == 0 {
		return nil
	}
	return dims
}

// nodesetZeroVariant returns a Variant holding the zero value (or an empty
// slice for arrays) for the given OPC UA DataType. It allows the importer to
// populate the mandatory Value attribute of Variable nodes with something
// readable so strict clients (e.g. UA SDK NodeGetHandleList) don't reject the
// node with BadAttributeIdInvalid. Falls back to int32(0) for unknown types.
func nodesetZeroVariant(dtID *ua.NodeID, isArray bool) *ua.Variant {
	if isArray {
		switch dtID.IntID() {
		case id.Boolean:
			return ua.MustVariant([]bool{})
		case id.SByte:
			return ua.MustVariant([]int8{})
		case id.Byte:
			return ua.MustVariant(ua.ByteArray{})
		case id.Int16:
			return ua.MustVariant([]int16{})
		case id.UInt16:
			return ua.MustVariant([]uint16{})
		case id.Int32:
			return ua.MustVariant([]int32{})
		case id.UInt32:
			return ua.MustVariant([]uint32{})
		case id.Int64:
			return ua.MustVariant([]int64{})
		case id.UInt64:
			return ua.MustVariant([]uint64{})
		case id.Float:
			return ua.MustVariant([]float32{})
		case id.Double:
			return ua.MustVariant([]float64{})
		case id.String:
			return ua.MustVariant([]string{})
		}
		return ua.MustVariant([]int32{})
	}
	switch dtID.IntID() {
	case id.Boolean:
		return ua.MustVariant(false)
	case id.SByte:
		return ua.MustVariant(int8(0))
	case id.Byte:
		return ua.MustVariant(byte(0))
	case id.Int16:
		return ua.MustVariant(int16(0))
	case id.UInt16:
		return ua.MustVariant(uint16(0))
	case id.Int32:
		return ua.MustVariant(int32(0))
	case id.UInt32:
		return ua.MustVariant(uint32(0))
	case id.Int64:
		return ua.MustVariant(int64(0))
	case id.UInt64:
		return ua.MustVariant(uint64(0))
	case id.Float:
		return ua.MustVariant(float32(0))
	case id.Double:
		return ua.MustVariant(float64(0))
	case id.String:
		return ua.MustVariant("")
	}
	return ua.MustVariant(int32(0))
}

func (srv *Server) ImportNodeSet(nodes *schema.UANodeSet) error {
	err := srv.namespacesImportNodeSet(nodes)
	if err != nil {
		return fmt.Errorf("problem creating namespaces: %w", err)
	}
	err = srv.nodesImportNodeSet(nodes)
	if err != nil {
		return fmt.Errorf("problem creating nodes: %w", err)
	}
	err = srv.refsImportNodeSet(nodes)
	if err != nil {
		return fmt.Errorf("problem creating references: %w", err)
	}
	return nil
}

func (srv *Server) namespacesImportNodeSet(nodes *schema.UANodeSet) error {
	if nodes.NamespaceUris == nil {
		return nil
	}
	for i := range nodes.NamespaceUris.Uri {
		_ = NewNodeNameSpace(srv, nodes.NamespaceUris.Uri[i])
	}
	return nil
}

func (srv *Server) nodesImportNodeSet(nodes *schema.UANodeSet) error {

	log.Printf("New Node Set: %s", nodes.LastModifiedAttr)

	// Pre-compute alias -> NodeID map so we can resolve DataType references
	// for variables/variable types declared with aliases in the XML.
	aliases := nodesetAliases(nodes)

	reftypes := make(map[string]*schema.UAReferenceType)

	// the first thing we have to do is go thorugh and define all the nodes.
	// set up the reference types.
	for i := range nodes.UAReferenceType {
		rt := nodes.UAReferenceType[i]
		reftypes[rt.BrowseNameAttr] = rt // sometimes they use browse name
		reftypes[rt.NodeIdAttr] = rt     // sometimes they use node id

		nid := ua.MustParseNodeID(rt.NodeIdAttr)

		var attrs Attributes = make(map[ua.AttributeID]*ua.DataValue)
		attrs[ua.AttributeIDAccessRestrictions] = DataValueFromValue(rt.AccessRestrictionsAttr)
		attrs[ua.AttributeIDBrowseName] = DataValueFromValue(&ua.QualifiedName{NamespaceIndex: nid.Namespace(), Name: rt.BrowseNameAttr})
		attrs[ua.AttributeIDIsAbstract] = DataValueFromValue(rt.IsAbstractAttr)
		attrs[ua.AttributeIDUserWriteMask] = DataValueFromValue(rt.UserWriteMaskAttr)
		attrs[ua.AttributeIDSymmetric] = DataValueFromValue(rt.SymmetricAttr)
		attrs[ua.AttributeIDWriteMask] = DataValueFromValue(rt.WriteMaskAttr)
		if len(rt.DisplayName) > 0 {
			attrs[ua.AttributeIDDisplayName] = DataValueFromValue(ua.NewLocalizedText(rt.DisplayName[0].Value))
		}
		if len(rt.InverseName) > 0 {
			attrs[ua.AttributeIDInverseName] = DataValueFromValue(ua.NewLocalizedText(rt.InverseName[0].Value))
		} else {
			attrs[ua.AttributeIDInverseName] = DataValueFromValue(ua.NewLocalizedText(""))
		}
		if len(rt.Description) > 0 {
			attrs[ua.AttributeIDDescription] = DataValueFromValue(ua.NewLocalizedText(rt.Description[0].Value))
		}
		attrs[ua.AttributeIDNodeClass] = DataValueFromValue(uint32(ua.NodeClassReferenceType))

		var refs References = make([]*ua.ReferenceDescription, 0)

		n := NewNode(nid, attrs, refs, nil)
		ns, err := srv.Namespace(int(nid.Namespace()))
		if err != nil {
			// This namespace doesn't exist.
			if srv.cfg.logger != nil {
				srv.cfg.logger.Warn("Could Not Find Namespace %d", nid.Namespace())
			}
			return err
		}
		ns.AddNode(n)
	}

	// set up the data types.
	for i := range nodes.UADataType {
		dt := nodes.UADataType[i]
		nid := ua.MustParseNodeID(dt.NodeIdAttr)

		var attrs Attributes = make(map[ua.AttributeID]*ua.DataValue)
		attrs[ua.AttributeIDAccessRestrictions] = DataValueFromValue(dt.AccessRestrictionsAttr)
		attrs[ua.AttributeIDBrowseName] = DataValueFromValue(&ua.QualifiedName{NamespaceIndex: nid.Namespace(), Name: dt.BrowseNameAttr})
		attrs[ua.AttributeIDIsAbstract] = DataValueFromValue(dt.IsAbstractAttr)
		attrs[ua.AttributeIDUserWriteMask] = DataValueFromValue(dt.UserWriteMaskAttr)
		attrs[ua.AttributeIDWriteMask] = DataValueFromValue(dt.WriteMaskAttr)
		if len(dt.DisplayName) > 0 {
			attrs[ua.AttributeIDDisplayName] = DataValueFromValue(ua.NewLocalizedText(dt.DisplayName[0].Value))
		}
		if len(dt.Description) > 0 {
			attrs[ua.AttributeIDDescription] = DataValueFromValue(ua.NewLocalizedText(dt.Description[0].Value))
		}
		attrs[ua.AttributeIDNodeClass] = DataValueFromValue(uint32(ua.NodeClassDataType))

		var refs References = make([]*ua.ReferenceDescription, 0)

		n := NewNode(nid, attrs, refs, nil)

		ns, err := srv.Namespace(int(nid.Namespace()))
		if err != nil {
			// This namespace doesn't exist.
			if srv.cfg.logger != nil {
				srv.cfg.logger.Warn("Could Not Find Namespace %d", nid.Namespace())
			}
			return err
		}
		ns.AddNode(n)
	}

	// set up the object types
	for i := range nodes.UAObjectType {
		ot := nodes.UAObjectType[i]
		nid := ua.MustParseNodeID(ot.NodeIdAttr)
		var attrs Attributes = make(map[ua.AttributeID]*ua.DataValue)
		attrs[ua.AttributeIDAccessRestrictions] = DataValueFromValue(ot.AccessRestrictionsAttr)
		attrs[ua.AttributeIDBrowseName] = DataValueFromValue(&ua.QualifiedName{NamespaceIndex: nid.Namespace(), Name: ot.BrowseNameAttr})
		attrs[ua.AttributeIDIsAbstract] = DataValueFromValue(ot.IsAbstractAttr)
		attrs[ua.AttributeIDUserWriteMask] = DataValueFromValue(ot.UserWriteMaskAttr)
		attrs[ua.AttributeIDWriteMask] = DataValueFromValue(ot.WriteMaskAttr)
		if len(ot.DisplayName) > 0 {
			attrs[ua.AttributeIDDisplayName] = DataValueFromValue(ua.NewLocalizedText(ot.DisplayName[0].Value))
		}
		if len(ot.Description) > 0 {
			attrs[ua.AttributeIDDescription] = DataValueFromValue(ua.NewLocalizedText(ot.Description[0].Value))
		}
		attrs[ua.AttributeIDNodeClass] = DataValueFromValue(uint32(ua.NodeClassObjectType))

		var refs References = make([]*ua.ReferenceDescription, 0)

		n := NewNode(nid, attrs, refs, nil)
		ns, err := srv.Namespace(int(nid.Namespace()))
		if err != nil {
			// This namespace doesn't exist.
			if srv.cfg.logger != nil {
				srv.cfg.logger.Warn("Could Not Find Namespace %d", nid.Namespace())
			}
			return err
		}
		ns.AddNode(n)
	}

	// set up the variable Types
	for i := range nodes.UAVariableType {
		ot := nodes.UAVariableType[i]
		nid := ua.MustParseNodeID(ot.NodeIdAttr)
		var attrs Attributes = make(map[ua.AttributeID]*ua.DataValue)
		attrs[ua.AttributeIDAccessRestrictions] = DataValueFromValue(ot.AccessRestrictionsAttr)
		attrs[ua.AttributeIDBrowseName] = DataValueFromValue(&ua.QualifiedName{NamespaceIndex: nid.Namespace(), Name: ot.BrowseNameAttr})
		attrs[ua.AttributeIDUserWriteMask] = DataValueFromValue(ot.UserWriteMaskAttr)
		attrs[ua.AttributeIDWriteMask] = DataValueFromValue(ot.WriteMaskAttr)
		if len(ot.DisplayName) > 0 {
			attrs[ua.AttributeIDDisplayName] = DataValueFromValue(ua.NewLocalizedText(ot.DisplayName[0].Value))
		}
		if len(ot.Description) > 0 {
			attrs[ua.AttributeIDDescription] = DataValueFromValue(ua.NewLocalizedText(ot.Description[0].Value))
		}
		attrs[ua.AttributeIDNodeClass] = DataValueFromValue(uint32(ua.NodeClassVariableType))
		// Mandatory VariableType attributes per OPC UA spec.
		attrs[ua.AttributeIDIsAbstract] = DataValueFromValue(ot.IsAbstractAttr)
		vtDT := nodesetResolveDataType(ot.DataTypeAttr, aliases)
		attrs[ua.AttributeIDDataType] = DataValueFromValue(vtDT)
		attrs[ua.AttributeIDValueRank] = DataValueFromValue(int32(ot.ValueRankAttr))
		if dims := nodesetParseArrayDimensions(ot.ArrayDimensionsAttr); dims != nil {
			attrs[ua.AttributeIDArrayDimensions] = DataValueFromValue(dims)
		}

		var refs References = make([]*ua.ReferenceDescription, 0)

		n := NewNode(nid, attrs, refs, nil)
		// Value attribute is mandatory and is stored through the value func,
		// not through the attrs map (see Node.Attribute). Populate a zero
		// value matching the declared DataType / ValueRank.
		_ = n.SetAttribute(ua.AttributeIDValue, &ua.DataValue{
			EncodingMask: ua.DataValueValue,
			Value:        nodesetZeroVariant(vtDT, ot.ValueRankAttr >= 1),
		})
		ns, err := srv.Namespace(int(nid.Namespace()))
		if err != nil {
			// This namespace doesn't exist.
			if srv.cfg.logger != nil {
				srv.cfg.logger.Warn("Could Not Find Namespace %d", nid.Namespace())
			}
			return err
		}
		ns.AddNode(n)
	}

	// set up the variables
	for i := range nodes.UAVariable {
		ot := nodes.UAVariable[i]
		nid := ua.MustParseNodeID(ot.NodeIdAttr)
		var attrs Attributes = make(map[ua.AttributeID]*ua.DataValue)
		attrs[ua.AttributeIDAccessRestrictions] = DataValueFromValue(ot.AccessRestrictionsAttr)
		attrs[ua.AttributeIDBrowseName] = DataValueFromValue(&ua.QualifiedName{NamespaceIndex: nid.Namespace(), Name: ot.BrowseNameAttr})
		attrs[ua.AttributeIDUserWriteMask] = DataValueFromValue(ot.UserWriteMaskAttr)
		attrs[ua.AttributeIDWriteMask] = DataValueFromValue(ot.WriteMaskAttr)
		if len(ot.DisplayName) > 0 {
			attrs[ua.AttributeIDDisplayName] = DataValueFromValue(ua.NewLocalizedText(ot.DisplayName[0].Value))
		}
		if len(ot.Description) > 0 {
			attrs[ua.AttributeIDDescription] = DataValueFromValue(ua.NewLocalizedText(ot.Description[0].Value))
		}
		attrs[ua.AttributeIDNodeClass] = DataValueFromValue(uint32(ua.NodeClassVariable))
		// Mandatory Variable attributes per OPC UA spec (Part 3, §5.6).
		// Without these strict clients (UA SDK NodeGetHandleList, etc.) abort
		// because reads return BadAttributeIdInvalid.
		varDT := nodesetResolveDataType(ot.DataTypeAttr, aliases)
		attrs[ua.AttributeIDDataType] = DataValueFromValue(varDT)
		attrs[ua.AttributeIDValueRank] = DataValueFromValue(int32(ot.ValueRankAttr))
		if dims := nodesetParseArrayDimensions(ot.ArrayDimensionsAttr); dims != nil {
			attrs[ua.AttributeIDArrayDimensions] = DataValueFromValue(dims)
		}
		attrs[ua.AttributeIDAccessLevel] = DataValueFromValue(byte(ot.AccessLevelAttr))
		attrs[ua.AttributeIDUserAccessLevel] = DataValueFromValue(byte(ot.UserAccessLevelAttr))
		attrs[ua.AttributeIDAccessLevelEx] = DataValueFromValue(ot.AccessLevelAttr)
		attrs[ua.AttributeIDMinimumSamplingInterval] = DataValueFromValue(ot.MinimumSamplingIntervalAttr)
		attrs[ua.AttributeIDHistorizing] = DataValueFromValue(ot.HistorizingAttr)

		var refs References = make([]*ua.ReferenceDescription, 0)

		n := NewNode(nid, attrs, refs, nil)
		// Value attribute is mandatory and is stored through the value func,
		// not through the attrs map (see Node.Attribute). Populate a zero
		// value matching the declared DataType / ValueRank.
		_ = n.SetAttribute(ua.AttributeIDValue, &ua.DataValue{
			EncodingMask: ua.DataValueValue,
			Value:        nodesetZeroVariant(varDT, ot.ValueRankAttr >= 1),
		})
		ns, err := srv.Namespace(int(nid.Namespace()))
		if err != nil {
			// This namespace doesn't exist.
			if srv.cfg.logger != nil {
				srv.cfg.logger.Warn("Could Not Find Namespace %d", nid.Namespace())
			}
			return err
		}
		ns.AddNode(n)
	}

	// set up the methods
	for i := range nodes.UAMethod {
		ot := nodes.UAMethod[i]
		nid := ua.MustParseNodeID(ot.NodeIdAttr)
		var attrs Attributes = make(map[ua.AttributeID]*ua.DataValue)
		attrs[ua.AttributeIDAccessRestrictions] = DataValueFromValue(ot.AccessRestrictionsAttr)
		attrs[ua.AttributeIDBrowseName] = DataValueFromValue(&ua.QualifiedName{NamespaceIndex: nid.Namespace(), Name: ot.BrowseNameAttr})
		attrs[ua.AttributeIDUserWriteMask] = DataValueFromValue(ot.UserWriteMaskAttr)
		attrs[ua.AttributeIDWriteMask] = DataValueFromValue(ot.WriteMaskAttr)
		if len(ot.DisplayName) > 0 {
			attrs[ua.AttributeIDDisplayName] = DataValueFromValue(ua.NewLocalizedText(ot.DisplayName[0].Value))
		}
		if len(ot.Description) > 0 {
			attrs[ua.AttributeIDDescription] = DataValueFromValue(ua.NewLocalizedText(ot.Description[0].Value))
		}
		attrs[ua.AttributeIDNodeClass] = DataValueFromValue(uint32(ua.NodeClassMethod))
		// Mandatory Method attributes per OPC UA spec (Part 3, §5.7).
		attrs[ua.AttributeIDExecutable] = DataValueFromValue(ot.ExecutableAttr)
		attrs[ua.AttributeIDUserExecutable] = DataValueFromValue(ot.UserExecutableAttr)

		var refs References = make([]*ua.ReferenceDescription, 0)

		n := NewNode(nid, attrs, refs, nil)
		ns, err := srv.Namespace(int(nid.Namespace()))
		if err != nil {
			// This namespace doesn't exist.
			if srv.cfg.logger != nil {
				srv.cfg.logger.Warn("Could Not Find Namespace %d", nid.Namespace())
			}
			return err
		}
		ns.AddNode(n)
	}

	// set up the objects
	for i := range nodes.UAObject {
		ot := nodes.UAObject[i]
		nid := ua.MustParseNodeID(ot.NodeIdAttr)
		if ot.NodeIdAttr == "i=85" {
			log.Printf("doing objects.")
		}
		var attrs Attributes = make(map[ua.AttributeID]*ua.DataValue)
		attrs[ua.AttributeIDAccessRestrictions] = DataValueFromValue(ot.AccessRestrictionsAttr)
		attrs[ua.AttributeIDBrowseName] = DataValueFromValue(&ua.QualifiedName{NamespaceIndex: nid.Namespace(), Name: ot.BrowseNameAttr})
		attrs[ua.AttributeIDUserWriteMask] = DataValueFromValue(ot.UserWriteMaskAttr)
		attrs[ua.AttributeIDWriteMask] = DataValueFromValue(ot.WriteMaskAttr)
		if len(ot.DisplayName) > 0 {
			attrs[ua.AttributeIDDisplayName] = DataValueFromValue(ua.NewLocalizedText(ot.DisplayName[0].Value))
		}
		if len(ot.Description) > 0 {
			attrs[ua.AttributeIDDescription] = DataValueFromValue(ua.NewLocalizedText(ot.Description[0].Value))
		}

		attrs[ua.AttributeIDNodeClass] = DataValueFromValue(uint32(ua.NodeClassObject))
		// Mandatory Object attribute per OPC UA spec (Part 3, §5.5).
		attrs[ua.AttributeIDEventNotifier] = DataValueFromValue(ot.EventNotifierAttr)

		var refs References = make([]*ua.ReferenceDescription, 0)

		n := NewNode(nid, attrs, refs, nil)
		ns, err := srv.Namespace(int(nid.Namespace()))
		if err != nil {
			// This namespace doesn't exist.
			if srv.cfg.logger != nil {
				srv.cfg.logger.Warn("Could Not Find Namespace %d", nid.Namespace())
			}
			return err
		}
		ns.AddNode(n)
	}

	// set up the views
	for i := range nodes.UAView {
		ot := nodes.UAView[i]
		nid := ua.MustParseNodeID(ot.NodeIdAttr)
		var attrs Attributes = make(map[ua.AttributeID]*ua.DataValue)
		attrs[ua.AttributeIDAccessRestrictions] = DataValueFromValue(ot.AccessRestrictionsAttr)
		attrs[ua.AttributeIDBrowseName] = DataValueFromValue(&ua.QualifiedName{NamespaceIndex: nid.Namespace(), Name: ot.BrowseNameAttr})
		attrs[ua.AttributeIDUserWriteMask] = DataValueFromValue(ot.UserWriteMaskAttr)
		attrs[ua.AttributeIDWriteMask] = DataValueFromValue(ot.WriteMaskAttr)
		if len(ot.DisplayName) > 0 {
			attrs[ua.AttributeIDDisplayName] = DataValueFromValue(ua.NewLocalizedText(ot.DisplayName[0].Value))
		}
		if len(ot.Description) > 0 {
			attrs[ua.AttributeIDDescription] = DataValueFromValue(ua.NewLocalizedText(ot.Description[0].Value))
		}
		attrs[ua.AttributeIDNodeClass] = DataValueFromValue(uint32(ua.NodeClassView))
		// Mandatory View attributes per OPC UA spec (Part 3, §5.8).
		attrs[ua.AttributeIDContainsNoLoops] = DataValueFromValue(ot.ContainsNoLoopsAttr)
		attrs[ua.AttributeIDEventNotifier] = DataValueFromValue(ot.EventNotifierAttr)

		var refs References = make([]*ua.ReferenceDescription, 0)

		n := NewNode(nid, attrs, refs, nil)
		ns, err := srv.Namespace(int(nid.Namespace()))
		if err != nil {
			if srv.cfg.logger != nil {
				srv.cfg.logger.Warn("Could Not Find Namespace %d", nid.Namespace())
			}
			return err
		}
		ns.AddNode(n)
	}

	return nil
}
func (srv *Server) refsImportNodeSet(nodes *schema.UANodeSet) error {

	log.Printf("New Node Set: %s", nodes.LastModifiedAttr)

	failures := 0
	reftypes := make(map[string]*schema.UAReferenceType)
	for i := range nodes.UAReferenceType {
		rt := nodes.UAReferenceType[i]
		reftypes[rt.BrowseNameAttr] = rt // sometimes they use browse name
		reftypes[rt.NodeIdAttr] = rt     // sometimes they use node id
	}

	aliases := make(map[string]string)
	for i := range nodes.Aliases.Alias {
		alias := nodes.Aliases.Alias[i]
		aliases[alias.AliasAttr] = alias.Value
	}

	// any of the aliases could be reference types, so we have to check them all and add them to the reftypes map
	// if they are.
	for alias := range aliases {
		aliasID := ua.MustParseNodeID(aliases[alias])
		refnode := srv.Node(aliasID)
		if refnode == nil {
			if srv.cfg.logger != nil {
				srv.cfg.logger.Warn("error loading alias %s", alias)
			}
			continue
		}
		rt := new(schema.UAReferenceType)
		rt.UAType = new(schema.UAType)
		rt.UAType.UANode = new(schema.UANode)
		rt.BrowseNameAttr = alias
		rt.NodeIdAttr = aliases[alias]
		isSymmetricValue, err := refnode.Attribute(ua.AttributeIDSymmetric)
		if err == nil {
			rt.SymmetricAttr = isSymmetricValue.Value.Value.Value().(bool)
		}

		_, ok := reftypes[alias]
		if !ok {
			reftypes[alias] = rt // sometimes they use browse name
		} else {
			if srv.cfg.logger != nil {
				srv.cfg.logger.Error("Duplicate reference type %s", alias)
			}
			continue
		}

		_, ok = reftypes[aliases[alias]]
		if !ok {
			reftypes[aliases[alias]] = rt // sometimes they use node id
		} else {
			if srv.cfg.logger != nil {
				srv.cfg.logger.Error("Duplicate reference type %s", aliases[alias])
			}
			continue
		}

	}

	// the first thing we have to do is go thorugh and define all the nodes.
	// set up the reference types.
	for i := range nodes.UAReferenceType {
		rt := nodes.UAReferenceType[i]

		nodeid := ua.MustParseNodeID(rt.NodeIdAttr)
		node := srv.Node(nodeid)
		if node == nil {
			log.Printf("Error loading node %s", rt.NodeIdAttr)
		}

		for rid := range rt.References.Reference {
			ref := rt.References.Reference[rid]
			refnodeid := ua.MustParseNodeID(ref.Value)
			n := srv.Node(refnodeid)
			if n == nil {
				log.Printf("can't find node %s as %s reference to %s", ref.Value, ref.ReferenceTypeAttr, rt.BrowseNameAttr)
				failures++
				continue
			}

			if ref.IsForwardAttr == nil {
				v := true
				ref.IsForwardAttr = &v
			}
			reftypeid := ua.MustParseNodeID(reftypes[ref.ReferenceTypeAttr].NodeIdAttr)
			node.AddRef(n, RefType(reftypeid.IntID()), *ref.IsForwardAttr)
			if !reftypes[ref.ReferenceTypeAttr].SymmetricAttr {
				n.AddRef(node, RefType(reftypeid.IntID()), !*ref.IsForwardAttr)
			}
		}

	}

	// set up the data types.
	for i := range nodes.UADataType {
		dt := nodes.UADataType[i]
		nid := ua.MustParseNodeID(dt.NodeIdAttr)
		node := srv.Node(nid)

		if nid.IntID() == 24 {
			log.Printf("doing BaseDataType")
		}

		for rid := range dt.References.Reference {
			ref := dt.References.Reference[rid]
			refnodeid := ua.MustParseNodeID(ref.Value)
			n := srv.Node(refnodeid)
			if n == nil {
				log.Printf("can't find node %s as %s reference to %s", ref.Value, ref.ReferenceTypeAttr, dt.BrowseNameAttr)
				failures++
				continue
			}

			if ref.IsForwardAttr == nil {
				v := true
				ref.IsForwardAttr = &v
			}

			reftypeid := ua.MustParseNodeID(reftypes[ref.ReferenceTypeAttr].NodeIdAttr)
			node.AddRef(n, RefType(reftypeid.IntID()), *ref.IsForwardAttr)
			if !reftypes[ref.ReferenceTypeAttr].SymmetricAttr {
				n.AddRef(node, RefType(reftypeid.IntID()), !*ref.IsForwardAttr)
			}

		}

	}

	// set up the object types
	for i := range nodes.UAObjectType {
		ot := nodes.UAObjectType[i]
		nid := ua.MustParseNodeID(ot.NodeIdAttr)
		node := srv.Node(nid)

		for rid := range ot.References.Reference {
			ref := ot.References.Reference[rid]
			refnodeid := ua.MustParseNodeID(ref.Value)
			n := srv.Node(refnodeid)
			if n == nil {
				log.Printf("can't find node %s as %s reference to %s", ref.Value, ref.ReferenceTypeAttr, ot.BrowseNameAttr)
				failures++
				continue
			}
			if ref.IsForwardAttr == nil {
				v := true
				ref.IsForwardAttr = &v
			}
			reftypeid := ua.MustParseNodeID(reftypes[ref.ReferenceTypeAttr].NodeIdAttr)
			node.AddRef(n, RefType(reftypeid.IntID()), *ref.IsForwardAttr)
			if !reftypes[ref.ReferenceTypeAttr].SymmetricAttr {
				n.AddRef(node, RefType(reftypeid.IntID()), !*ref.IsForwardAttr)
			}
		}
	}

	// set up the variable Types
	for i := range nodes.UAVariableType {
		ot := nodes.UAVariableType[i]
		nid := ua.MustParseNodeID(ot.NodeIdAttr)
		node := srv.Node(nid)

		for rid := range ot.References.Reference {
			ref := ot.References.Reference[rid]
			refnodeid := ua.MustParseNodeID(ref.Value)
			n := srv.Node(refnodeid)
			if n == nil {
				log.Printf("can't find node %s as %s reference to %s", ref.Value, ref.ReferenceTypeAttr, ot.BrowseNameAttr)
				failures++
				continue
			}
			if ref.IsForwardAttr == nil {
				v := true
				ref.IsForwardAttr = &v
			}
			reftypeid := ua.MustParseNodeID(reftypes[ref.ReferenceTypeAttr].NodeIdAttr)
			node.AddRef(n, RefType(reftypeid.IntID()), *ref.IsForwardAttr)
			if !reftypes[ref.ReferenceTypeAttr].SymmetricAttr {
				n.AddRef(node, RefType(reftypeid.IntID()), !*ref.IsForwardAttr)
			}

		}

	}

	// set up the variables
	for i := range nodes.UAVariable {
		ot := nodes.UAVariable[i]
		nid := ua.MustParseNodeID(ot.NodeIdAttr)
		node := srv.Node(nid)

		for rid := range ot.References.Reference {
			ref := ot.References.Reference[rid]
			refnodeid := ua.MustParseNodeID(ref.Value)
			n := srv.Node(refnodeid)
			if n == nil {
				log.Printf("can't find node %s as %s reference to %s", ref.Value, ref.ReferenceTypeAttr, ot.BrowseNameAttr)
				failures++
				continue
			}
			if ref.IsForwardAttr == nil {
				v := true
				ref.IsForwardAttr = &v
			}
			reftypeid := ua.MustParseNodeID(reftypes[ref.ReferenceTypeAttr].NodeIdAttr)
			node.AddRef(n, RefType(reftypeid.IntID()), *ref.IsForwardAttr)
			if !reftypes[ref.ReferenceTypeAttr].SymmetricAttr {
				n.AddRef(node, RefType(reftypeid.IntID()), !*ref.IsForwardAttr)
			}

		}

	}

	// set up the methods
	for i := range nodes.UAMethod {
		ot := nodes.UAMethod[i]
		nid := ua.MustParseNodeID(ot.NodeIdAttr)
		node := srv.Node(nid)

		for rid := range ot.References.Reference {
			ref := ot.References.Reference[rid]
			refnodeid := ua.MustParseNodeID(ref.Value)
			n := srv.Node(refnodeid)
			if n == nil {
				log.Printf("can't find node %s as %s reference to %s", ref.Value, ref.ReferenceTypeAttr, ot.BrowseNameAttr)
				failures++
				continue
			}
			if ref.IsForwardAttr == nil {
				v := true
				ref.IsForwardAttr = &v
			}
			reftypeid := ua.MustParseNodeID(reftypes[ref.ReferenceTypeAttr].NodeIdAttr)
			node.AddRef(n, RefType(reftypeid.IntID()), *ref.IsForwardAttr)
			if !reftypes[ref.ReferenceTypeAttr].SymmetricAttr {
				n.AddRef(node, RefType(reftypeid.IntID()), !*ref.IsForwardAttr)
			}
		}

	}

	// set up the objects
	for i := range nodes.UAObject {
		ot := nodes.UAObject[i]
		nid := ua.MustParseNodeID(ot.NodeIdAttr)
		node := srv.Node(nid)
		if ot.NodeIdAttr == "i=84" {
			log.Printf("doing root.")
		}

		for rid := range ot.References.Reference {
			ref := ot.References.Reference[rid]
			refnodeid := ua.MustParseNodeID(ref.Value)
			n := srv.Node(refnodeid)
			if n == nil {
				log.Printf("can't find node %s as %s reference to %s", ref.Value, ref.ReferenceTypeAttr, ot.BrowseNameAttr)
				failures++
				continue
			}
			if ref.IsForwardAttr == nil {
				v := true
				ref.IsForwardAttr = &v
			}
			reftypeid := ua.MustParseNodeID(reftypes[ref.ReferenceTypeAttr].NodeIdAttr)
			node.AddRef(n, RefType(reftypeid.IntID()), *ref.IsForwardAttr)
			if !reftypes[ref.ReferenceTypeAttr].SymmetricAttr {
				n.AddRef(node, RefType(reftypeid.IntID()), !*ref.IsForwardAttr)
			}

		}

	}

	// set up the views
	for i := range nodes.UAView {
		ot := nodes.UAView[i]
		nid := ua.MustParseNodeID(ot.NodeIdAttr)
		node := srv.Node(nid)
		if node == nil || ot.References == nil {
			continue
		}
		for rid := range ot.References.Reference {
			ref := ot.References.Reference[rid]
			refnodeid := ua.MustParseNodeID(ref.Value)
			n := srv.Node(refnodeid)
			if n == nil {
				log.Printf("can't find node %s as %s reference to %s", ref.Value, ref.ReferenceTypeAttr, ot.BrowseNameAttr)
				failures++
				continue
			}
			if ref.IsForwardAttr == nil {
				v := true
				ref.IsForwardAttr = &v
			}
			reftypeid := ua.MustParseNodeID(reftypes[ref.ReferenceTypeAttr].NodeIdAttr)
			node.AddRef(n, RefType(reftypeid.IntID()), *ref.IsForwardAttr)
			if !reftypes[ref.ReferenceTypeAttr].SymmetricAttr {
				n.AddRef(node, RefType(reftypeid.IntID()), !*ref.IsForwardAttr)
			}
		}
	}

	return nil
}
