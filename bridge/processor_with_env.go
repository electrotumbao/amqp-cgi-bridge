package bridge

import "context"

func ProcessorWithEnv(p Processor, env map[string]string) Processor {
	return func(ctx context.Context, headers map[string]string, body []byte) error {
		if headers == nil {
			headers = make(map[string]string)
		}

		for k, v := range env {
			if _, ok := headers[k]; !ok {
				headers[k] = v
			}
		}

		return p(ctx, headers, body)
	}
}
