package whatsapp

import (
	"github.com/andre25costa-code/kuromatsu/pkg/bus"
	"github.com/andre25costa-code/kuromatsu/pkg/channels"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

func init() {
	channels.RegisterFactory(
		config.ChannelWhatsApp,
		func(channelName, channelType string, cfg *config.Config, b *bus.MessageBus) (channels.Channel, error) {
			bc := cfg.Channels[channelName]
			decoded, err := bc.GetDecoded()
			if err != nil {
				return nil, err
			}
			c, ok := decoded.(*config.WhatsAppSettings)
			if !ok {
				return nil, channels.ErrSendFailed
			}
			return NewWhatsAppChannel(bc, c, b)
		},
	)
}
