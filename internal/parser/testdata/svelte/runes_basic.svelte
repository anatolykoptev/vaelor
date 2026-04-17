<script>
  let count = $state(0);
  let doubled = $derived(count * 2);
  $effect(() => console.log(count));
  let { name = "anon" } = $props();
  let raw = $state.raw([]);
  let sum = $derived.by(() => count + 1);
  $effect.pre(() => console.log("pre"));
  $effect.root(() => {});
  let id = $props.id();
  $inspect.with(count, console.log);
  let val = $bindable(0);
  $inspect(count);
</script>
