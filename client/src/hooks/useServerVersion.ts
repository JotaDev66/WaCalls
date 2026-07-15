import { useQuery } from "@tanstack/react-query";
import { fetchServerVersion } from "@/services/version";

export const useServerVersion = () =>
  useQuery({
    queryKey: ["server-version"],
    queryFn: fetchServerVersion,
    staleTime: Infinity,
    retry: 1,
    refetchOnWindowFocus: false,
  });
