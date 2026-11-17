import { api, http } from "@/shared/lib/axios";
import { createAxiosMutator } from "./createAxiosMutator";

export const axiosMutator = createAxiosMutator(api, http);

export type { BodyType, ErrorType } from "./createAxiosMutator";
