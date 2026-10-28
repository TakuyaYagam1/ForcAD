package v1

import (
	"context"
	"fmt"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
)

type eventsHandler struct{ events EventService }

func (*eventsHandler) OpenEventStream(context.Context) (*api.OpenEventStreamSwitchingProtocols, error) {
	const location = "httpv1.eventsHandler.OpenEventStream"
	return nil, fmt.Errorf("%s - websocket route is not handled here", location)
}
