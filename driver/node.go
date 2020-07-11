package mfs

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/container-storage-interface/spec/lib/go/csi"

	"k8s.io/utils/mount"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type NodeServer struct {
	Driver  *mfsDriver
	mounter mount.Interface
	root    string
}

func NewNodeServer(n *mfsDriver, mounter mount.Interface, root string) *NodeServer {
	if root == "" {
		root = "/"
	}
	return &NodeServer{
		Driver:  n,
		mounter: mounter,
		root:    root,
	}
}

// Register node server to the grpc server
func (ns *NodeServer) Register(srv *grpc.Server) {
	csi.RegisterNodeServer(srv, ns)
}

func (ns *NodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	switch "" {
	case req.GetTargetPath(),
		req.GetVolumeId():
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.GetVolumeCapability() == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	fmt.Println("volume capabilitiy:", req.GetVolumeCapability().GetAccessMode().String())

	tgt := req.GetTargetPath()
	notMnt, err := ns.mounter.IsLikelyNotMountPoint(tgt)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(tgt, 0750); err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}
			notMnt = true
		} else {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}
	if !notMnt {
		return &csi.NodePublishVolumeResponse{}, nil
	}

	volCtx := req.GetVolumeContext()
	srv := volCtx["server"]
	if srv == "" {
		srv = ns.Driver.mfsServer
	}
	srvPath := volCtx["path"]
	if srvPath == "" {
		srvPath = "/"
	}
	srvPath = filepath.Join(ns.root, srvPath)

	src := fmt.Sprintf("%s:%s", srv, srvPath)
	mo := req.GetVolumeCapability().GetMount().GetMountFlags()
	if req.GetReadonly() {
		mo = append(mo, "ro")
	}
	log.Println("mounting:", src, ns.Driver.mfsServer, tgt)
	switch err = ns.mounter.Mount(src, tgt, "moosefs", mo); {
	case err == nil:
		// success
	case os.IsPermission(err):
		return nil, status.Error(codes.PermissionDenied, err.Error())
	case strings.Contains(err.Error(), "invalid argument"):
		return nil, status.Error(codes.InvalidArgument, err.Error())
	default:
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func (ns *NodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	switch "" {
	case req.GetVolumeId():
		return nil, status.Error(codes.InvalidArgument, "volume id not provided")
	case req.GetTargetPath():
		return nil, status.Error(codes.InvalidArgument, "target path not provided")
	}

	if notMnt, err := ns.mounter.IsLikelyNotMountPoint(req.GetTargetPath()); err != nil {
		switch {
		case os.IsNotExist(err):
			return &csi.NodeUnpublishVolumeResponse{}, nil
		case errors.Is(err, syscall.ENOTCONN):
			// mfsmount process failed. Unmount still needs to be called to clear the
			// fusemount record with the os.
		default:
			return nil, status.Error(codes.Internal, err.Error())
		}
	} else if notMnt {
		return nil, status.Error(codes.NotFound, "volume not mounted")
	}

	if err := mount.CleanupMountPoint(req.GetTargetPath(), ns.mounter, false); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (ns *NodeServer) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	return &csi.NodeGetInfoResponse{NodeId: ns.Driver.nodeID}, nil
}

func (ns *NodeServer) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: []*csi.NodeServiceCapability{
			{Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: csi.NodeServiceCapability_RPC_UNKNOWN,
				},
			}},
		},
	}, nil
}

func (ns *NodeServer) NodeGetVolumeStats(ctx context.Context, in *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (ns *NodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (ns *NodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	return &csi.NodeStageVolumeResponse{}, nil
}

func (ns *NodeServer) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}