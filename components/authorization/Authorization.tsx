"use client";

import Link from "next/link";
import styles from "./Authorization.module.css";
import cn from "classnames";
import { paths } from "@/paths";
import toast, { Toaster } from "react-hot-toast";
import { useRouter } from "next/navigation";

interface IAuthorizationProps {
  isLogin?: boolean;
  isRegister?: boolean;
  isRecover?: boolean;
}

const successToast = () =>
  toast.success("Successfully created!", {
    style: {
      backgroundColor: "var(--background-light-theme)",
      marginTop: "65px",
    },
  });

const errorToast = () =>
  toast.error("Incorrect data", {
    style: {
      backgroundColor: "var(--background-light-theme)",
      marginTop: "65px",
    },
  });

const Authorization = ({
  isLogin = false,
  isRegister = false,
  isRecover = false,
}: IAuthorizationProps): JSX.Element => {
  const router = useRouter();

  return (
    <div className={styles.wrapper}>
      <Toaster />
      <div
        className={cn(
          styles.container,
          isRegister && styles.isRegister,
          isRecover && styles.isRecover
        )}
      >
        <h2 className={styles.header2}>
          {isLogin
            ? "Login"
            : isRegister
            ? "Register"
            : isRecover
            ? "Recover"
            : null}
        </h2>
        <h3 className={styles.header3}>
          {isLogin
            ? "Log in to BLOB"
            : isRegister
            ? "Register in BLOB"
            : isRecover
            ? "Recover account in BLOB"
            : null}
        </h3>
        <form action="" method="get">
          <label htmlFor="phone">
            <input
              className={styles.input}
              type="tel"
              name="phone"
              id="phone"
              required
              placeholder="Enter your phone"
            />
          </label>
          <label htmlFor="password">
            {!isRecover && (
              <input
                className={styles.input}
                type="password"
                name="password"
                id="password"
                required
                placeholder="Enter your password"
              />
            )}

            {isRegister && (
              <input
                className={styles.input}
                type="password"
                name="confirm_password"
                id="confirm_password"
                required
                placeholder="Confirm your password"
              />
            )}
          </label>

          <button
            className={styles.btnSubmit}
            type="submit"
            onClick={() => errorToast()}
          >
            {isLogin
              ? "Log in"
              : isRegister
              ? "Register"
              : isRecover
              ? "Recover"
              : null}
          </button>
        </form>

        <div className={styles.problemsWrapper}>
          {isLogin ? (
            <>
              <p className={styles.toRegister}>
                Don`t have account yet? You can{" "}
                <Link
                  className={cn(styles.toRegister, styles.link)}
                  href={paths.authorization.register}
                >
                  register
                </Link>
              </p>

              <p className={styles.toRegister}>
                Forgot password?{" "}
                <Link
                  className={cn(styles.toRegister, styles.link)}
                  href={paths.authorization.recover}
                >
                  Recover
                </Link>
              </p>
            </>
          ) : isRegister ? (
            <p className={styles.toRegister}>
              Already have an account?{" "}
              <Link
                className={cn(styles.toRegister, styles.link)}
                href={paths.authorization.login}
              >
                login
              </Link>
            </p>
          ) : isRecover ? (
            <p className={styles.toRegister}>
              Do you remember the password?{" "}
              <Link
                className={cn(styles.toRegister, styles.link)}
                href={paths.authorization.login}
              >
                login
              </Link>
            </p>
          ) : null}
        </div>
      </div>
    </div>
  );
};

export default Authorization;
