/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Footer } from "@/components/Footer";
import { BlurFade } from "@/components/magicui/blur-fade";
import { ShineBorder } from "@/components/magicui/shine-border";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Logo } from "@/components/ui/Logo";
import { Separator } from "@/components/ui/separator";
import { useAuth } from "@/hooks/useAuth";
import { api } from "@/lib/api";
import { useForm } from "@tanstack/react-form";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Fingerprint } from "lucide-react";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export function Login() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { login, isLoggingIn, loginError, setIsAuthenticated } = useAuth();

  // Query to check if setup is required
  const { data: setupRequired } = useQuery({
    queryKey: ["setup-required"],
    queryFn: () => api.checkSetupRequired(),
    staleTime: Infinity,
    retry: false,
    refetchOnWindowFocus: false,
  });

  // Query to check if OIDC is enabled
  const { data: oidcConfig } = useQuery({
    queryKey: ["oidc-config"],
    queryFn: () => api.getOIDCConfig(),
    staleTime: Infinity,
    retry: false,
    refetchOnWindowFocus: false,
  });

  useEffect(() => {
    // Redirect to setup if required
    if (setupRequired) {
      navigate({ to: "/setup" });
      return;
    }

    // Check if this is an OIDC callback
    const urlParams = new URLSearchParams(window.location.search);
    const code = urlParams.get("code");
    const state = urlParams.get("state");

    if (code && state) {
      // This is an OIDC callback, validate the session
      api
        .validate()
        .then(() => {
          // If validation succeeds, set the user as logged in
          setIsAuthenticated(true);
          navigate({ to: "/" });
        })
        .catch((error) => {
          // If validation fails, show an error
          toast.error(error.message || t("login.oidc.failure"));
        });
    }
  }, [setupRequired, navigate, setIsAuthenticated, t]);

  const form = useForm({
    defaultValues: {
      username: "",
      password: "",
      rememberMe: true,
    },
    onSubmit: async ({ value }) => {
      login(value);
    },
  });

  const handleOIDCLogin = () => {
    if (oidcConfig?.enabled && oidcConfig.authorizationUrl) {
      window.location.href = oidcConfig.authorizationUrl;
    }
  };

  // Show loading state while checking OIDC config
  if (oidcConfig === null) {
    return (
      <div className="flex h-screen items-center justify-center bg-background">
        <div className="text-center">{t("common.loading")}</div>
      </div>
    );
  }

  // Don't show built-in login form if OIDC is enabled and built-in login is disabled
  const showBuiltInLogin =
    !oidcConfig?.enabled || !oidcConfig?.disableBuiltInLogin;
  const showOIDC = oidcConfig?.enabled;

  return (
    <div className="flex h-screen items-center justify-center bg-background px-4 sm:px-6">
      <BlurFade
        duration={0.5}
        delay={0.1}
        blur="10px"
        className="w-full max-w-md"
      >
        <Card className="relative overflow-hidden w-full shadow-xl">
          <ShineBorder
            shineColor={["var(--chart-1)", "var(--chart-2)", "var(--chart-3)"]}
          />
          <CardHeader className="text-center">
            <div className="flex items-center justify-center mb-2">
              <Logo className="h-12 w-12" />
            </div>
            <CardTitle className="text-3xl font-bold pointer-events-none select-none">
              qui
            </CardTitle>
            <CardDescription className="pointer-events-none select-none">
              {t("login.description")}
            </CardDescription>
          </CardHeader>
          <CardContent className="pt-6">
            {showBuiltInLogin && (
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  form.handleSubmit();
                }}
                className="space-y-4"
              >
                <form.Field
                  name="username"
                  validators={{
                    onChange: ({ value }) => {
                      if (!value) return t("login.username.required");
                      return undefined;
                    },
                  }}
                >
                  {(field) => (
                    <div className="space-y-2">
                      <Label htmlFor={field.name}>{t("common.username")}</Label>
                      <Input
                        id={field.name}
                        type="text"
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onChange={(e) => field.handleChange(e.target.value)}
                        placeholder={t("login.username.placeholder")}
                      />
                      {field.state.meta.isTouched &&
                        field.state.meta.errors[0] && (
                          <p className="text-sm text-destructive">
                            {field.state.meta.errors[0]}
                          </p>
                        )}
                    </div>
                  )}
                </form.Field>

                <form.Field
                  name="password"
                  validators={{
                    onChange: ({ value }) => {
                      if (!value) return t("login.password.required");
                      return undefined;
                    },
                  }}
                >
                  {(field) => (
                    <div className="space-y-2">
                      <Label htmlFor={field.name}>{t("common.password")}</Label>
                      <Input
                        id={field.name}
                        type="password"
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onChange={(e) => field.handleChange(e.target.value)}
                        placeholder={t("login.password.placeholder")}
                      />
                      {field.state.meta.isTouched &&
                        field.state.meta.errors[0] && (
                          <p className="text-sm text-destructive">
                            {field.state.meta.errors[0]}
                          </p>
                        )}
                    </div>
                  )}
                </form.Field>

                <form.Field name="rememberMe">
                  {(field) => (
                    <div className="flex items-center space-x-2">
                      <Checkbox
                        id={field.name}
                        checked={field.state.value}
                        onCheckedChange={(checked) =>
                          field.handleChange(checked === true)
                        }
                      />
                      <Label
                        htmlFor={field.name}
                        className="text-sm font-normal leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70"
                      >
                        {t("login.rememberMe")}
                      </Label>
                    </div>
                  )}
                </form.Field>

                {loginError && (
                  <div className="bg-destructive/10 border border-destructive/20 text-destructive px-4 py-3 rounded-md text-sm">
                    {typeof loginError === "string"
                      ? loginError
                      : loginError.message?.includes("Invalid credentials") ||
                        loginError.message?.includes("401")
                      ? t("login.error.invalidCredentials")
                      : loginError.message || t("login.error.generic")}
                  </div>
                )}

                <form.Subscribe
                  selector={(state) => [state.canSubmit, state.isSubmitting]}
                >
                  {([canSubmit, isSubmitting]) => (
                    <Button
                      type="submit"
                      className="w-full"
                      size="lg"
                      disabled={!canSubmit || isSubmitting || isLoggingIn}
                    >
                      {isLoggingIn
                        ? t("login.buttons.loggingIn")
                        : t("login.buttons.signIn")}
                    </Button>
                  )}
                </form.Subscribe>
              </form>
            )}

            {showBuiltInLogin && showOIDC && (
              <div className="relative my-6">
                <div className="absolute inset-0 flex items-center">
                  <Separator className="w-full" />
                </div>
                <div className="relative flex justify-center text-xs uppercase">
                  <span className="bg-background px-2 text-muted-foreground">
                    {t("login.oidc.separator")}
                  </span>
                </div>
              </div>
            )}

            {showOIDC && (
              <Button
                type="button"
                variant="outline"
                className="w-full"
                size="lg"
                onClick={handleOIDCLogin}
              >
                <Fingerprint className="mr-2 h-5 w-5" />
                {t("login.oidc.button")}
              </Button>
            )}

            <Footer />
          </CardContent>
        </Card>
      </BlurFade>
    </div>
  );
}