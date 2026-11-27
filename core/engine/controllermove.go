package engine

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/l3ony2k/twinnel/core/foreign"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/core/store"
)

// ControllerPortLayerID is the override controller.move writes.
const ControllerPortLayerID = "controller-port"

// firstMovePort is where controller.move starts looking for a free port
// (9090 is mihomo's default and the GUI clients'); any port in use is
// skipped.
const firstMovePort = 9097

// portFree reports whether nothing listens on 127.0.0.1:port (a listen
// that is closed at once).
var portFree = func(port int) bool {
	l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	l.Close()
	return true
}

// MoveController moves Twinnel's controller to a free port on 127.0.0.1:
// the override controller-port sets external-controller, settings follow,
// the config is written (an apply on the new address: nothing answers
// there yet, so nothing is reloaded, and the other program's mihomo on
// the old address is never touched), and Twinnel's mihomo service is
// restarted when it is installed, so it listens on the new port.
func (e *Engine) MoveController(ctx context.Context, p rpc.ControllerMoveParams) (rpc.ControllerMoveResult, error) {
	var out rpc.ControllerMoveResult
	st, err := e.Store.Settings()
	if err != nil {
		return out, err
	}
	out.From = controllerAddr(st.Mihomo.Controller)
	from := foreign.Port(out.From)
	port := p.Port
	if port != 0 {
		if port < 1024 || port > 65535 || port == from {
			return out, rpc.ErrInvalid("port %d: pick a free port from 1024 to 65535 other than %d", port, from)
		}
		if !portFree(port) {
			return out, rpc.ErrInvalid("port %d is in use", port)
		}
	} else {
		for q := firstMovePort; q < firstMovePort+200; q++ {
			if q != from && portFree(q) {
				port = q
				break
			}
		}
		if port == 0 {
			return out, rpc.Errorf(rpc.CodeFailed, "no free port from %d to %d", firstMovePort, firstMovePort+199)
		}
	}
	out.To = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	if p.DryRun {
		out.DryRun = true
		return out, nil
	}
	if _, ok := st.ActiveSubscription(); !ok {
		return out, rpc.ErrInvalid("no active subscription: nothing to write the new port into")
	}

	prevLayer, hadLayer := e.storedLayer(st, ControllerPortLayerID)
	content := "# Twinnel: the controller moved off " + out.From + " (another program's mihomo held it).\nexternal-controller: " + out.To + "\n"
	if _, err := e.PutLayer(rpc.LayerPutParams{ID: ControllerPortLayerID, Name: "Controller port", Content: content}); err != nil {
		return out, err
	}
	if _, err := e.Store.UpdateSettings(func(s *store.Settings) error { s.Mihomo.Controller = out.To; return nil }); err != nil {
		return out, err
	}
	revert := func() {
		if hadLayer {
			_, _ = e.PutLayer(rpc.LayerPutParams{ID: ControllerPortLayerID, Name: prevLayer.Name, Content: prevLayer.Content})
		} else {
			_ = e.DeleteLayer(ControllerPortLayerID)
		}
		_, _ = e.Store.UpdateSettings(func(s *store.Settings) error { s.Mihomo.Controller = st.Mihomo.Controller; return nil })
	}
	ar, aerr := e.Apply(ctx, rpc.ApplyParams{Reason: "controller.move to " + out.To, ReasonCode: store.ReasonCtlMove, ReasonArgs: []string{out.To}, Force: true})
	out.Apply = ar
	if aerr != nil {
		revert()
		return out, aerr
	}
	// The other program keeps the old port; Twinnel forgets it held it.
	e.clearForeign()
	if e.Services != nil && st.Mihomo.ServiceLabel != "" {
		if s, err := e.Services.Status(ctx, st.Mihomo.ServiceLabel); err == nil && s.Installed {
			if _, err := e.ServiceOp(ctx, "mihomo", service.OpRestart); err != nil {
				out.Warnings = append(out.Warnings, fmt.Sprintf("the config was written, but restarting mihomo failed (%v); restart it to use %s", err, out.To))
			} else {
				out.Restarted = true
			}
		}
	}
	e.triggerHealth("controller moved")
	return out, nil
}

type layerCopy struct{ Name, Content string }

// storedLayer returns a stored layer's name and content.
func (e *Engine) storedLayer(st *store.Settings, id string) (layerCopy, bool) {
	for _, l := range st.Layers {
		if l.ID == id {
			b, err := e.Store.ReadLayer(id)
			if err != nil {
				return layerCopy{}, false
			}
			return layerCopy{Name: l.Name, Content: string(b)}, true
		}
	}
	return layerCopy{}, false
}
