# WhatsApp GPT Call Integration

This project integrates kevit.io's WhatsApp API with OpenAI's real-time voice API, allowing users to have voice calls with an AI assistant through WhatsApp.

## Features

- Receive WhatsApp call webhooks
- Connect WhatsApp calls to OpenAI's real-time voice API
- Structured logging with slog
- Configurable environment variables

## Prerequisites

- Go 1.21 or higher
- OpenAI API key with access to real-time voice models
- kevit.io's WhatsApp API token
- Publicly accessible endpoint for webhook reception

## Environment Variables

| Variable | Description | Required | Default |
|----------|-------------|----------|---------|
| PORT | Port on which the server listens | No | 8080 |
| OPENAI_API_KEY | OpenAI API key | Yes | - |
| WHATSAPP_TOKEN | kevit.io's WhatsApp API token | Yes | - |
| DEBUG | Enable debug logging | No | false |

## Setup and Running

1. Clone the repository
2. Set the required environment variables
3. Run the application:
   ```
   go run main.go
   ```

## Webhook Configuration

Configure your kevit.io's WhatsApp API to forward call events to your webhook endpoint:

```
https://your-domain.com/webhook
```

## Security Considerations

- Never commit API keys or tokens to the repository
- Use HTTPS for all API communications
- Consider implementing rate limiting for webhook endpoints
- Validate all incoming webhooks

## Contact Information

For more details about this integration or any questions, please reach out to me directly.

## License

This project is licensed under the MIT License - see the LICENSE file for details.
