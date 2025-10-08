package dns

import (
	pb "github.com/agentuity/go-common/dns/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (a *DNSAddAction) ToProto() *pb.AddRequest {
	return &pb.AddRequest{
		Name:           a.Name,
		Type:           a.Type,
		Value:          a.Value,
		TtlSeconds:     int64(a.TTL.Seconds()),
		ExpiresSeconds: int64(a.Expires.Seconds()),
		Priority:       int32(a.Priority),
		Weight:         int32(a.Weight),
		Port:           int32(a.Port),
	}
}

func (a *DNSDeleteAction) ToProto() *pb.DeleteRequest {
	return &pb.DeleteRequest{
		Name: a.Name,
		Ids:  a.IDs,
	}
}

func (a *DNSCertAction) ToProto() *pb.CertRequest {
	return &pb.CertRequest{
		Name: a.Name,
	}
}

func FromProtoAddResponse(resp *pb.AddResponse) (*DNSRecord, error) {
	if !resp.Success {
		return nil, &DNSError{Message: resp.Error}
	}
	return &DNSRecord{IDs: []string{resp.Id}}, nil
}

func FromProtoDeleteResponse(resp *pb.DeleteResponse) error {
	if !resp.Success {
		return &DNSError{Message: resp.Error}
	}
	return nil
}

func FromProtoCertResponse(resp *pb.CertResponse) (*DNSCert, error) {
	if !resp.Success {
		return nil, &DNSError{Message: resp.Error}
	}
	return &DNSCert{
		Certificate: resp.Certificate,
		PrivateKey:  resp.PrivateKey,
		Expires:     resp.Expires.AsTime(),
		Domain:      resp.Domain,
	}, nil
}

type DNSError struct {
	Message string
}

func (e *DNSError) Error() string {
	return e.Message
}

func ToProtoAddResponse(record *DNSRecord, err error) *pb.AddResponse {
	resp := &pb.AddResponse{}
	if err != nil {
		resp.Success = false
		resp.Error = err.Error()
	} else {
		resp.Success = true
		if record != nil && len(record.IDs) > 0 {
			resp.Id = record.IDs[0]
		}
	}
	return resp
}

func ToProtoDeleteResponse(err error) *pb.DeleteResponse {
	resp := &pb.DeleteResponse{}
	if err != nil {
		resp.Success = false
		resp.Error = err.Error()
	} else {
		resp.Success = true
	}
	return resp
}

func ToProtoCertResponse(cert *DNSCert, err error) *pb.CertResponse {
	resp := &pb.CertResponse{}
	if err != nil {
		resp.Success = false
		resp.Error = err.Error()
	} else {
		resp.Success = true
		if cert != nil {
			resp.Certificate = cert.Certificate
			resp.PrivateKey = cert.PrivateKey
			resp.Expires = timestamppb.New(cert.Expires)
			resp.Domain = cert.Domain
		}
	}
	return resp
}
