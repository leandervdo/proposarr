import { useEffect, useRef } from "react";

/** False once the component has unmounted (its dialog closed or moved to another title). */
export function useMounted() {
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  return mounted;
}
