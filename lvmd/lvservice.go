package lvmd

import (
	"context"

	"github.com/cybozu-go/log"
	"github.com/topolvm/topolvm/lvmd/command"
	"github.com/topolvm/topolvm/lvmd/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// NewLVService creates a new LVServiceServer
func NewLVService(mapper *DeviceClassManager, notifyFunc func()) proto.LVServiceServer {
	return &lvService{
		mapper:     mapper,
		notifyFunc: notifyFunc,
	}
}

type lvService struct {
	proto.UnimplementedLVServiceServer
	mapper     *DeviceClassManager
	notifyFunc func()
}

func (s *lvService) notify() {
	if s.notifyFunc == nil {
		return
	}
	s.notifyFunc()
}

func (s *lvService) CreateLV(_ context.Context, req *proto.CreateLVRequest) (*proto.CreateLVResponse, error) {
	dc, err := s.mapper.DeviceClass(req.DeviceClass)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%s: %s", err.Error(), req.DeviceClass)
	}
	vg, err := command.FindVolumeGroup(dc.VolumeGroup)
	if err != nil {
		return nil, err
	}
	requested := req.GetSizeGb() << 30
	free, err := vg.Free()
	if err != nil {
		log.Error("failed to free VG", map[string]interface{}{
			log.FnError: err,
		})
		return nil, status.Error(codes.Internal, err.Error())
	}

	if free < requested {
		log.Error("no enough space left on VG", map[string]interface{}{
			"free":      free,
			"requested": requested,
		})
		return nil, status.Errorf(codes.ResourceExhausted, "no enough space left on VG: free=%d, requested=%d", free, requested)
	}

	var stripe uint
	if dc.Stripe != nil {
		stripe = *dc.Stripe
	}
	var lvcreateOptions []string
	if dc.LVCreateOptions != nil {
		lvcreateOptions = dc.LVCreateOptions
	}

	lv, err := vg.CreateVolume(req.GetName(), requested, req.GetTags(), stripe, dc.StripeSize, lvcreateOptions)
	if err != nil {
		log.Error("failed to create volume", map[string]interface{}{
			"name":      req.GetName(),
			"requested": requested,
			"tags":      req.GetTags(),
		})
		return nil, status.Error(codes.Internal, err.Error())
	}
	s.notify()

	log.Info("created a new LV", map[string]interface{}{
		"name": req.GetName(),
		"size": requested,
	})

	return &proto.CreateLVResponse{
		Volume: &proto.LogicalVolume{
			Name:     lv.Name(),
			SizeGb:   lv.Size() >> 30,
			DevMajor: lv.MajorNumber(),
			DevMinor: lv.MinorNumber(),
		},
	}, nil
}

func (s *lvService) RemoveLV(_ context.Context, req *proto.RemoveLVRequest) (*proto.Empty, error) {
	dc, err := s.mapper.DeviceClass(req.DeviceClass)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%s: %s", err.Error(), req.DeviceClass)
	}
	vg, err := command.FindVolumeGroup(dc.VolumeGroup)
	if err != nil {
		return nil, err
	}
	lvs, err := vg.ListVolumes()
	if err != nil {
		log.Error("failed to list volumes", map[string]interface{}{
			log.FnError: err,
		})
		return nil, status.Error(codes.Internal, err.Error())
	}

	for _, lv := range lvs {
		if lv.Name() != req.GetName() {
			continue
		}

		err = lv.Remove()
		if err != nil {
			log.Error("failed to remove volume", map[string]interface{}{
				log.FnError: err,
				"name":      lv.Name(),
			})
			return nil, status.Error(codes.Internal, err.Error())
		}
		s.notify()

		log.Info("removed a LV", map[string]interface{}{
			"name": req.GetName(),
		})
		break
	}

	return &proto.Empty{}, nil
}

func (s *lvService) CreateLVSnapshot(_ context.Context, req *proto.CreateLVSnapshotRequest) (*proto.CreateLVSnapshotResponse, error) {
	dc, err := s.mapper.DeviceClass(req.DeviceClass)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%s: %s", err.Error(), req.DeviceClass)
	}
	vg, err := command.FindVolumeGroup(dc.VolumeGroup)
	if err != nil {
		return nil, err
	}

	// Fetch the source logical volume
	sourceVolume := req.GetSourcevolume()
	sourceLV, err := vg.FindVolume(sourceVolume)
	if err == command.ErrNotFound {
		log.Error("source logical volume is not found", map[string]interface{}{
			log.FnError: err,
			"name":      sourceVolume,
		})
		return nil, status.Errorf(codes.NotFound, "source logical volume %s is not found", sourceVolume)
	}
	if err != nil {
		log.Error("failed to find source volume", map[string]interface{}{
			log.FnError: err,
			"name":      sourceVolume,
		})
		return nil, status.Error(codes.Internal, err.Error())
	}

	var requested uint64
	if sourceLV.IsThin() {
		// In case of thin-snapshots, the size is the same as the source volume.
		requested = sourceLV.Size()
	} else {
		requested = req.GetSizeGb() << 30
	}

	// In case of snapshots, the size is the same as the source volume.

	free, err := vg.Free()
	if err != nil {
		log.Error("failed to get free space of VG", map[string]interface{}{
			log.FnError: err,
		})
		return nil, status.Error(codes.Internal, err.Error())
	}

	if free < requested {
		log.Error("no enough space left on VG", map[string]interface{}{
			"free":      free,
			"requested": requested,
		})
		return nil, status.Errorf(codes.ResourceExhausted, "no enough space left on VG: free=%d, requested=%d", free, requested)
	}

	// Create snapshot lv
	snapLV, err := sourceLV.Snapshot(req.GetName(), requested)
	if err != nil {
		log.Error("failed to create snap volume", map[string]interface{}{
			log.FnError: err,
			"name":      req.GetName(),
		})
		return nil, status.Error(codes.Internal, err.Error())
	}
	// If source volume is thin, activate the thin snapshot lv with accessmode.
	if sourceLV.IsThin() {
		if err := snapLV.Activate(req.AccessType); err != nil {
			log.Error("failed to activate snap volume", map[string]interface{}{
				log.FnError: err,
				"name":      req.GetName(),
			})
			return nil, status.Error(codes.Internal, err.Error())
		}
	}
	s.notify()

	log.Info("created a new snapshot LV", map[string]interface{}{
		"name":       req.GetName(),
		"size":       requested,
		"accessType": req.AccessType,
		"dataSource": sourceVolume,
	})

	return &proto.CreateLVSnapshotResponse{
		Snap: &proto.LogicalVolume{
			Name:     snapLV.Name(),
			SizeGb:   snapLV.Size() >> 30,
			DevMajor: snapLV.MajorNumber(),
			DevMinor: snapLV.MinorNumber(),
		},
	}, nil

}

func (s *lvService) ResizeLV(_ context.Context, req *proto.ResizeLVRequest) (*proto.Empty, error) {
	dc, err := s.mapper.DeviceClass(req.DeviceClass)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%s: %s", err.Error(), req.DeviceClass)
	}
	vg, err := command.FindVolumeGroup(dc.VolumeGroup)
	if err != nil {
		return nil, err
	}
	lv, err := vg.FindVolume(req.GetName())
	if err == command.ErrNotFound {
		log.Error("logical volume is not found", map[string]interface{}{
			log.FnError: err,
			"name":      req.GetName(),
		})
		return nil, status.Errorf(codes.NotFound, "logical volume %s is not found", req.GetName())
	}
	if err != nil {
		log.Error("failed to find volume", map[string]interface{}{
			log.FnError: err,
			"name":      req.GetName(),
		})
		return nil, status.Error(codes.Internal, err.Error())
	}

	requested := req.GetSizeGb() << 30
	current := lv.Size()

	if requested < current {
		log.Error("shrinking volume size is not allowed", map[string]interface{}{
			log.FnError: err,
			"name":      req.GetName(),
			"requested": requested,
			"current":   current,
		})
		return nil, status.Error(codes.OutOfRange, "shrinking volume size is not allowed")
	}

	free, err := vg.Free()
	if err != nil {
		log.Error("failed to free VG", map[string]interface{}{
			log.FnError: err,
			"name":      req.GetName(),
		})
		return nil, status.Error(codes.Internal, err.Error())
	}
	if free < (requested - current) {
		log.Error("no enough space left on VG", map[string]interface{}{
			log.FnError: err,
			"name":      req.GetName(),
			"requested": requested,
			"current":   current,
			"free":      free,
		})
		return nil, status.Errorf(codes.ResourceExhausted, "no enough space left on VG: free=%d, requested=%d", free, requested-current)
	}

	err = lv.Resize(requested)
	if err != nil {
		log.Error("failed to resize LV", map[string]interface{}{
			log.FnError: err,
			"name":      req.GetName(),
			"requested": requested,
			"current":   current,
			"free":      free,
		})
		return nil, status.Error(codes.Internal, err.Error())
	}
	s.notify()

	log.Info("resized a LV", map[string]interface{}{
		"name": req.GetName(),
		"size": requested,
	})

	return &proto.Empty{}, nil
}
