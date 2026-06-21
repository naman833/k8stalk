package sanitize

// This file documents what gets masked per resource type.
// The actual masking is done by pattern matching in sanitize.go.
//
// Resource-specific masking rules:
//
// Pods:
//   - Container image names (registry URLs)
//   - Environment variable values in error messages
//   - IP addresses (pod IPs, node IPs)
//   - Volume mount paths that contain usernames
//
// Events:
//   - IP addresses in event messages
//   - Hostnames/URLs in messages
//   - Base64 tokens that appear in error output
//
// Services:
//   - ClusterIP, ExternalIP, LoadBalancer IPs
//   - Endpoint IPs
//
// Ingress:
//   - Hostnames, TLS secret names
//   - Backend service URLs
//
// Nodes:
//   - Node names (often contain internal hostnames)
//   - IP addresses (InternalIP, ExternalIP)
//   - Kernel version strings
//
// ConfigMaps/Secrets:
//   - All values (never send secret data)
//   - Key names are kept for diagnosis context
//
// General:
//   - Namespace names that look like environments (prod-*, staging-*)
//     are NOT masked — they're needed for context
//   - Resource names are NOT masked — they're needed for correlation
