package agent

import (
	"net/http"
	"syscall"

	"github.com/labstack/echo"
	"github.com/pkg/errors"

	"github.com/determined-ai/determined/master/internal/api"
	"github.com/determined-ai/determined/master/internal/scheduler"
	"github.com/determined-ai/determined/master/pkg/actor"
	aproto "github.com/determined-ai/determined/master/pkg/agent"
	"github.com/determined-ai/determined/master/pkg/check"
	"github.com/determined-ai/determined/master/pkg/container"
	"github.com/determined-ai/determined/master/pkg/device"
	proto "github.com/determined-ai/determined/proto/pkg/apiv1"
)

type slot struct {
	cluster   *actor.Ref
	device    device.Device
	enabled   slotEnabled
	container *container.Container
}

type slotEnabled struct {
	deviceAdded  bool
	agentEnabled bool
	userEnabled  bool
}

func (s slotEnabled) Enabled() bool {
	return s.agentEnabled && s.userEnabled
}

type slotSummary struct {
	ID        string               `json:"id"`
	Device    device.Device        `json:"device"`
	Enabled   bool                 `json:"enabled"`
	Container *container.Container `json:"container"`
}

type (
	patchSlot struct {
		Enabled bool `json:"enabled"`
	}
)

func (s *slot) Receive(ctx *actor.Context) error {
	switch msg := ctx.Message().(type) {
	case actor.PreStart:
		s.patch(ctx)
	case slotSummary:
		ctx.Respond(s.summarize(ctx))
	case patchSlot:
		s.enabled.userEnabled = msg.Enabled
		s.patch(ctx)
	case aproto.StartContainer:
		check.Panic(check.True(s.enabled.Enabled(), "container assigned but slot is not enabled"))
		check.Panic(check.True(s.container == nil, "container already assigned to slot"))
		s.container = &msg.Container
	case aproto.ContainerStateChanged:
		check.Panic(check.Equal(s.container.ID, msg.Container.ID, "Invalid container id sent to slot"))
		s.container = &msg.Container
		if msg.Container.State == container.Terminated {
			s.container = nil
			if s.enabled.Enabled() {
				ctx.Tell(s.cluster, scheduler.FreeDevice{DeviceID: s.deviceID(ctx)})
			}
		}
	case *proto.GetSlotRequest:
		ctx.Respond(&proto.GetSlotResponse{Slot: toProtoSlot(s.summarize(ctx))})
	case *proto.EnableSlotRequest:
		s.enabled.userEnabled = true
		s.patch(ctx)
		ctx.Respond(&proto.EnableSlotResponse{Slot: toProtoSlot(s.summarize(ctx))})
	case *proto.DisableSlotRequest:
		s.enabled.userEnabled = false
		s.patch(ctx)
		ctx.Respond(&proto.DisableSlotResponse{Slot: toProtoSlot(s.summarize(ctx))})
	case echo.Context:
		s.handleAPIRequest(ctx, msg)
	case actor.PostStop:
		s.enabled.agentEnabled = false
		s.patch(ctx)
	default:
		return actor.ErrUnexpectedMessage(ctx)
	}
	return nil
}

func (s *slot) handleAPIRequest(ctx *actor.Context, apiCtx echo.Context) {
	switch apiCtx.Request().Method {
	case echo.GET:
		ctx.Respond(apiCtx.JSON(http.StatusOK, s.summarize(ctx)))
	case echo.PATCH:
		patch := patchSlot{}
		if err := api.BindPatch(&patch, apiCtx); err != nil {
			ctx.Respond(errors.Wrap(err, "error patching slot"))
			return
		}
		s.enabled.userEnabled = patch.Enabled
		s.patch(ctx)
		ctx.Respond(apiCtx.NoContent(http.StatusNoContent))
	default:
		ctx.Respond(echo.ErrMethodNotAllowed)
	}
}

func (s *slot) patch(ctx *actor.Context) {
	if s.enabled.Enabled() && !s.enabled.deviceAdded {
		s.enabled.deviceAdded = true
		add := scheduler.AddDevice{DeviceID: s.deviceID(ctx)}
		if s.container != nil {
			add.ContainerID = &s.container.ID
		}
		ctx.Tell(s.cluster, add)
	} else if !s.enabled.Enabled() && s.enabled.deviceAdded {
		s.enabled.deviceAdded = false
		remove := scheduler.RemoveDevice{DeviceID: s.deviceID(ctx)}
		ctx.Tell(s.cluster, remove)
		if s.container != nil {
			kill := aproto.SignalContainer{ContainerID: s.container.ID, Signal: syscall.SIGKILL}
			ctx.Tell(remove.Agent, kill)
		}
	}
}

func (s *slot) deviceID(ctx *actor.Context) scheduler.DeviceID {
	return scheduler.DeviceID{Agent: ctx.Self().Parent().Parent(), Device: s.device}
}

func (s *slot) summarize(ctx *actor.Context) slotSummary {
	return slotSummary{
		ID:        ctx.Self().Address().Local(),
		Device:    s.device,
		Enabled:   s.enabled.Enabled(),
		Container: s.container,
	}
}
