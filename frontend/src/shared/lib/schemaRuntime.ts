import { z } from "zod";

// Run before application schemas are constructed. Zod's optional JIT probe
// otherwise attempts dynamic code generation even when strict CSP rejects it.
z.config({ jitless: true });
