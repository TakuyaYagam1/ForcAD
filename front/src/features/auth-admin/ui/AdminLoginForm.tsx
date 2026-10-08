import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { zodResolver } from "@hookform/resolvers/zod";
import { useNavigate, useLocation } from "react-router-dom";
import { Eye, EyeOff, ArrowRight } from "lucide-react";
import { useAdminLoginMutation } from "../model/useAdminLoginMutation";
import { useAdminAuthStore } from "../model/useAdminAuth";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { BrandIcon } from "@/shared/ui/brand/BrandIcon";

const schema = z.object({
  username: z.string().min(1, "Введите логин"),
  password: z.string().min(1, "Введите пароль"),
});
type FormValues = z.infer<typeof schema>;

export function AdminLoginForm() {
  const navigate = useNavigate();
  const location = useLocation();
  const { mutateAsync, isPending } = useAdminLoginMutation();
  const authError = useAdminAuthStore((state) => state.error);
  const isAuthenticated = useAdminAuthStore((state) => state.isAuthenticated);
  const checked = useAdminAuthStore((state) => state.isAuthChecked);
  const checkSession = useAdminAuthStore((state) => state.checkSession);
  useEffect(() => {
    if (!checked) void checkSession();
  }, [checked, checkSession]);
  const [showPassword, setShowPassword] = useState(false);
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<FormValues>({ resolver: zodResolver(schema) });
  useEffect(() => {
    if (!isAuthenticated) return;
    const state = location.state as {
      from?: string | { pathname?: string };
    } | null;
    const from =
      typeof state?.from === "string" ? state.from : state?.from?.pathname;
    navigate(from || "/admin/scoreboard", { replace: true });
  }, [isAuthenticated, navigate, location.state]);
  const onSubmit = async (values: FormValues) => {
    try {
      await mutateAsync(values);
    } catch {
      /* The auth store supplies the visible error. */
    }
  };
  return (
    <section className="login-card">
      <BrandIcon name="check" />
      <h2>Вход в управление</h2>
      <p>Панель организатора Кубка Федерации.</p>
      <form
        onSubmit={handleSubmit(onSubmit)}
        className="space-y-5 admin-form border-0 p-0 bg-transparent"
      >
        {authError && (
          <div className="notice" role="alert">
            {authError}
          </div>
        )}
        <div className="space-y-2">
          <Label htmlFor="username">Логин</Label>
          <Input
            id="username"
            autoComplete="username"
            placeholder="Логин администратора"
            aria-invalid={Boolean(errors.username)}
            aria-describedby={errors.username ? "username-error" : undefined}
            {...register("username")}
          />
          {errors.username && (
            <p id="username-error" className="text-xs text-red-300">
              {errors.username.message}
            </p>
          )}
        </div>
        <div className="space-y-2">
          <Label htmlFor="password">Пароль</Label>
          <div className="relative">
            <Input
              id="password"
              type={showPassword ? "text" : "password"}
              autoComplete="current-password"
              className="pr-10"
              aria-invalid={Boolean(errors.password)}
              aria-describedby={errors.password ? "password-error" : undefined}
              {...register("password")}
            />
            <button
              type="button"
              className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground"
              aria-label={showPassword ? "Скрыть пароль" : "Показать пароль"}
              onClick={() => setShowPassword(!showPassword)}
            >
              {showPassword ? <EyeOff size={15} /> : <Eye size={15} />}
            </button>
          </div>
          {errors.password && (
            <p id="password-error" className="text-xs text-red-300">
              {errors.password.message}
            </p>
          )}
        </div>
        <Button type="submit" className="w-full" disabled={isPending}>
          {isPending ? "Входим…" : "Войти"}
          <ArrowRight size={15} />
        </Button>
      </form>
    </section>
  );
}
