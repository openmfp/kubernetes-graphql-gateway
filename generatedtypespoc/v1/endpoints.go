package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type EndpointsList struct {
	Items []Endpoints `json:"items"` // List of endpoints.
	// I needed to add the official metav1 types
	// to avoid implementing the runtime.Object methods
	metav1.ListMeta `json:"metadata,omitempty"` // Standard list metadata.
	metav1.TypeMeta `json:",inline"`
}

func (in *EndpointsList) DeepCopyInto(out *EndpointsList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Endpoints, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

func (in *EndpointsList) DeepCopy() *EndpointsList {
	if in == nil {
		return nil
	}
	out := new(EndpointsList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject() runtime.Object is required
func (in *EndpointsList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

type Endpoints struct {
	metav1.TypeMeta `json:",inline"`
	Subsets         []EndpointSubset `json:"subsets,omitempty"` // List of subsets as strings.
}

func (in *Endpoints) DeepCopyInto(out *Endpoints) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	if in.Subsets != nil {
		in, out := &in.Subsets, &out.Subsets
		*out = make([]EndpointSubset, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

func (in *Endpoints) DeepCopy() *Endpoints {
	if in == nil {
		return nil
	}
	out := new(Endpoints)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject() runtime.Object is required
func (in *Endpoints) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

type EndpointSubset struct {
	Addresses         []EndpointAddress `json:"addresses,omitempty"`
	NotReadyAddresses []EndpointAddress `json:"notReadyAddresses,omitempty"`
	Ports             []EndpointPort    `json:"ports,omitempty"`
}

func (in *EndpointSubset) DeepCopyInto(out *EndpointSubset) {
	*out = *in
	if in.Addresses != nil {
		in, out := &in.Addresses, &out.Addresses
		*out = make([]EndpointAddress, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.NotReadyAddresses != nil {
		in, out := &in.NotReadyAddresses, &out.NotReadyAddresses
		*out = make([]EndpointAddress, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Ports != nil {
		in, out := &in.Ports, &out.Ports
		*out = make([]EndpointPort, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

func (in *EndpointSubset) DeepCopy() *EndpointSubset {
	if in == nil {
		return nil
	}
	out := new(EndpointSubset)
	in.DeepCopyInto(out)
	return out
}

type EndpointAddress struct {
	Hostname string  `json:"hostname,omitempty"`
	IP       string  `json:"ip"`
	NodeName *string `json:"nodeName,omitempty"`
	// I needed to add the official corev1 type
	// to avoid implementing the runtime.Object methods
	TargetRef *corev1.ObjectReference `json:"targetRef,omitempty"`
}

func (in *EndpointAddress) DeepCopyInto(out *EndpointAddress) {
	*out = *in
	if in.NodeName != nil {
		in, out := &in.NodeName, &out.NodeName
		*out = new(string)
		**out = **in
	}
	if in.TargetRef != nil {
		in, out := &in.TargetRef, &out.TargetRef
		*out = new(corev1.ObjectReference)
		**out = **in
	}
}

func (in *EndpointAddress) DeepCopy() *EndpointAddress {
	if in == nil {
		return nil
	}
	out := new(EndpointAddress)
	in.DeepCopyInto(out)
	return out
}

type EndpointPort struct {
	AppProtocol *string `json:"appProtocol,omitempty"`
	Name        string  `json:"name,omitempty"`
	Port        int32   `json:"port"`
	Protocol    string  `json:"protocol,omitempty"`
}

func (in *EndpointPort) DeepCopyInto(out *EndpointPort) {
	*out = *in
	if in.AppProtocol != nil {
		in, out := &in.AppProtocol, &out.AppProtocol
		*out = new(string)
		**out = **in
	}
}

func (in *EndpointPort) DeepCopy() *EndpointPort {
	if in == nil {
		return nil
	}
	out := new(EndpointPort)
	in.DeepCopyInto(out)
	return out
}
