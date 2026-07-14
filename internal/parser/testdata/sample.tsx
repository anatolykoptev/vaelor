import { useState } from "react";

export function Counter(props: { start: number }): JSX.Element {
  const [count, setCount] = useState(props.start);
  const bump = () => setCount(count + 1);
  return <button onClick={bump}>{format(count)}</button>;
}

function format(n: number): string {
  return String(n);
}
