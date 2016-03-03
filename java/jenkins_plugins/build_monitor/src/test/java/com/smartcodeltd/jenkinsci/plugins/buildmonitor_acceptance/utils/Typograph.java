package com.smartcodeltd.jenkinsci.plugins.buildmonitor_acceptance.utils;

/**
 * @author Jan Molak
 */
public class Typograph {
    public static String deCamelCase(String camelCasedString) {
        return camelCasedString.replaceAll(
                String.format("%s|%s|%s",
                        "(?<=[A-Z])(?=[A-Z][a-z])",
                        "(?<=[^A-Z])(?=[A-Z])",
                        "(?<=[A-Za-z])(?=[^A-Za-z])"
                ),
            " "
        );
    }
    
    public static String de_snake_case(String snake_cased_string) {
        return snake_cased_string.replaceAll("_", " ");
    }
}
